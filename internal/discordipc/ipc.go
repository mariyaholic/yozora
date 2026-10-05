//go:build windows

// Package discordipc implements Discord's local Rich Presence IPC: the
// discord-ipc-{n} named pipe, the handshake, SET_ACTIVITY and keepalive.
package discordipc

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Microsoft/go-winio"
)

// Opcodes per the current official RPC docs.
const (
	opHandshake = 0
	opFrame     = 1
	opClose     = 2
	opPing      = 3
	opPong      = 4
)

const ipcWriteTimeout = 2 * time.Second

// Activity types.
const (
	TypePlaying   = 0
	TypeStreaming = 1
	TypeListening = 2
	TypeWatching  = 3
)

type Timestamps struct {
	Start int64 `json:"start,omitempty"`
	End   int64 `json:"end,omitempty"`
}

type Assets struct {
	LargeImage string `json:"large_image,omitempty"`
	LargeText  string `json:"large_text,omitempty"`
	SmallImage string `json:"small_image,omitempty"`
	SmallText  string `json:"small_text,omitempty"`
}

type Button struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type Activity struct {
	Name              string      `json:"name,omitempty"`
	Type              int         `json:"type,omitempty"`
	StatusDisplayType int         `json:"status_display_type,omitempty"`
	Details           string      `json:"details,omitempty"`
	State             string      `json:"state,omitempty"`
	Timestamps        *Timestamps `json:"timestamps,omitempty"`
	Assets            *Assets     `json:"assets,omitempty"`
	Buttons           []Button    `json:"buttons,omitempty"`
	Instance          bool        `json:"instance,omitempty"`
}

type frame struct {
	Op    int             `json:"-"`
	Body  []byte          `json:"-"`
	Args  json.RawMessage `json:"args,omitempty"`
	Cmd   string          `json:"cmd,omitempty"`
	Nonce string          `json:"nonce,omitempty"`
	Evt   *string         `json:"evt,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error json.RawMessage `json:"error,omitempty"`
	Code  int             `json:"code,omitempty"`
	Msg2  string          `json:"message,omitempty"`
}

// Transport is the byte pipe abstraction (named pipe in production, net.Pipe
// in tests).
type Transport interface {
	io.ReadWriteCloser
}

func pipeName(index int) string { return fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, index) }

func dialPipe(path string) (Transport, error) {
	// An overlapped handle lets the read loop wait without blocking writes.
	// os.OpenFile creates a synchronous Windows handle and stalls full-duplex IPC.
	timeout := 250 * time.Millisecond
	return winio.DialPipe(path, &timeout)
}

// Dial opens the first available Discord pipe (indices 0..9).
func Dial() (Transport, error) {
	var lastErr error
	for i := 0; i < 10; i++ {
		f, err := dialPipe(pipeName(i))
		if err == nil {
			return f, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("discordipc: no discord pipe found (is Discord running?): %w", lastErr)
}

type Client struct {
	transport Transport
	clientID  string

	mu           sync.Mutex
	writeMu      sync.Mutex
	pending      map[string]chan *frame
	nonce        atomic.Int64
	readOnce     sync.Once
	accepted     *Activity
	acceptedData json.RawMessage

	closed atomic.Bool
}

// NewClient builds a client over an existing transport (used by tests).
func NewClient(t Transport, clientID string) *Client {
	return &Client{transport: t, clientID: clientID, pending: map[string]chan *frame{}}
}

// Connect dials Discord and performs the handshake.
func Connect(clientID string) (*Client, error) {
	t, err := Dial()
	if err != nil {
		return nil, err
	}
	c := NewClient(t, clientID)
	if err := c.handshake(); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) writeRaw(op int, payload []byte) error {
	return c.writeRawTimeout(op, payload, ipcWriteTimeout)
}

func (c *Client) writeRawTimeout(op int, payload []byte, timeout time.Duration) error {
	packet := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(packet[0:4], uint32(op))
	binary.LittleEndian.PutUint32(packet[4:8], uint32(len(payload)))
	copy(packet[8:], payload)

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed.Load() {
		return errors.New("discordipc: client closed")
	}

	type result struct {
		n   int
		err error
	}
	written := make(chan result, 1)
	go func() {
		n, err := c.transport.Write(packet)
		written <- result{n: n, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-written:
		if r.err != nil {
			return r.err
		}
		if r.n != len(packet) {
			return io.ErrShortWrite
		}
		return nil
	case <-timer.C:
		c.Close()
		return fmt.Errorf("discordipc: write timed out after %s", timeout)
	}
}

func (c *Client) handshake() error {
	hello, _ := json.Marshal(map[string]any{"v": 1, "client_id": c.clientID})
	if err := c.writeRaw(opHandshake, hello); err != nil {
		return fmt.Errorf("discordipc: handshake write: %w", err)
	}
	f, err := c.recvWithTimeout(5 * time.Second)
	if err != nil {
		return fmt.Errorf("discordipc: handshake read: %w", err)
	}
	if f.Evt == nil || *f.Evt != "READY" {
		return fmt.Errorf("discordipc: expected READY, got cmd=%q evt=%v", f.Cmd, f.Evt)
	}
	c.readOnce.Do(func() { go c.readLoop() })
	return nil
}

func (c *Client) nextNonce() string {
	return strconv.FormatInt(c.nonce.Add(1), 10)
}

func (c *Client) request(f *frame, timeout time.Duration) (*frame, error) {
	nonce := c.nextNonce()
	f.Nonce = nonce
	payload, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	ch := make(chan *frame, 1)
	c.mu.Lock()
	c.pending[nonce] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, nonce)
		c.mu.Unlock()
	}()
	if err := c.writeRaw(opFrame, payload); err != nil {
		return nil, err
	}
	select {
	case resp := <-ch:
		if resp.Evt != nil && *resp.Evt == "ERROR" {
			var rpcErr struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			if len(resp.Data) > 0 {
				if err := json.Unmarshal(resp.Data, &rpcErr); err != nil {
					return nil, fmt.Errorf("discordipc: %s returned malformed error data: %w", f.Cmd, err)
				}
			}
			if rpcErr.Code == 0 {
				rpcErr.Code = resp.Code
			}
			if rpcErr.Message == "" {
				rpcErr.Message = resp.Msg2
			}
			if rpcErr.Message == "" {
				rpcErr.Message = "Discord rejected the command"
			}
			return nil, fmt.Errorf("discordipc: %s failed: code=%d %s", f.Cmd, rpcErr.Code, rpcErr.Message)
		}
		if resp.Code != 0 && len(resp.Data) == 0 {
			return nil, fmt.Errorf("discordipc: %s failed: code=%d %s", f.Cmd, resp.Code, resp.Msg2)
		}
		return resp, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("discordipc: %s timed out", f.Cmd)
	}
}

// recv reads one frame from the transport with a deadline.
func (c *Client) recvWithTimeout(timeout time.Duration) (*frame, error) {
	type result struct {
		f   *frame
		err error
	}
	done := make(chan result, 1)
	go func() {
		f, err := c.recv()
		done <- result{f: f, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-done:
		return r.f, r.err
	case <-timer.C:
		c.Close()
		return nil, fmt.Errorf("read timed out after %s", timeout)
	}
}

func (c *Client) recv() (*frame, error) {
	head := make([]byte, 8)
	if err := readFull(c.transport, head); err != nil {
		return nil, err
	}
	op := int(binary.LittleEndian.Uint32(head[0:]))
	n := binary.LittleEndian.Uint32(head[4:])
	if n > 1<<20 {
		return nil, fmt.Errorf("discordipc: frame too large: %d", n)
	}
	body := make([]byte, n)
	if n > 0 {
		if err := readFull(c.transport, body); err != nil {
			return nil, err
		}
	}
	f := &frame{Op: op, Body: body}
	if len(body) > 0 {
		if err := json.Unmarshal(body, f); err != nil {
			return nil, fmt.Errorf("discordipc: bad frame json: %w", err)
		}
	}
	return f, nil
}

func readFull(r io.Reader, buf []byte) error {
	_, err := io.ReadFull(r, buf)
	return err
}

func (c *Client) readLoop() {
	for {
		f, err := c.recv()
		if err != nil {
			if c.closed.Load() {
				return
			}
			c.failPending("transport closed: " + err.Error())
			c.Close()
			return
		}
		switch {
		case f.Op == opPing:
			if err := c.writeRaw(opPong, f.Body); err != nil {
				c.failPending("PING response failed: " + err.Error())
				c.Close()
				return
			}
		case f.Op == opPong:
			// keepalive ack
		case f.Op == opClose:
			c.failPending("Discord closed the IPC connection")
			c.Close()
			return
		case f.Nonce != "":
			c.mu.Lock()
			ch, ok := c.pending[f.Nonce]
			c.mu.Unlock()
			if ok {
				select {
				case ch <- f:
				default:
				}
			}
		default:
			// events (READY, ActivityJoin, ...) ignored for now
		}
	}
}

// SetActivity sends SET_ACTIVITY; nil clears presence.
func (c *Client) SetActivity(a *Activity) error {
	args := map[string]any{"pid": os.Getpid(), "activity": a}
	resp, err := c.request(&frame{Cmd: "SET_ACTIVITY", Args: marshalArgs(args)}, 5*time.Second)
	if err != nil {
		return err
	}
	if resp.Cmd != "SET_ACTIVITY" {
		return fmt.Errorf("discordipc: expected SET_ACTIVITY reply, got %q", resp.Cmd)
	}
	var accepted *Activity
	if a != nil && len(resp.Data) > 0 {
		if err := json.Unmarshal(resp.Data, &accepted); err != nil {
			// Discord may echo button labels rather than button objects. Keep
			// the raw readback; optional decoding must not reject an accepted send.
			accepted = nil
		}
	}
	c.mu.Lock()
	c.accepted = accepted
	c.acceptedData = append(json.RawMessage(nil), resp.Data...)
	c.mu.Unlock()
	return nil
}

// AcceptedActivity reads back the last activity echoed by Discord.
func (c *Client) AcceptedActivity() *Activity {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accepted == nil {
		return nil
	}
	a := *c.accepted
	if a.Assets != nil {
		assets := *a.Assets
		a.Assets = &assets
	}
	if a.Timestamps != nil {
		timestamps := *a.Timestamps
		a.Timestamps = &timestamps
	}
	a.Buttons = append([]Button(nil), a.Buttons...)
	return &a
}

// AcceptedPayload returns the exact successful Discord activity readback.
func (c *Client) AcceptedPayload() json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append(json.RawMessage(nil), c.acceptedData...)
}

func marshalArgs(args map[string]any) json.RawMessage {
	b, _ := json.Marshal(args)
	return b
}

// Ping sends the keepalive PING (op 3) per the current protocol.
func (c *Client) Ping() error {
	return c.writeRaw(opPing, []byte(`{"v":1}`))
}

// Alive reports whether the transport is still open.
func (c *Client) Alive() bool { return !c.closed.Load() }

func (c *Client) failPending(message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for nonce, ch := range c.pending {
		select {
		case ch <- &frame{Cmd: "ERROR", Code: -1, Msg2: message}:
		default:
		}
		delete(c.pending, nonce)
	}
}

// Close shuts the client down.
func (c *Client) Close() {
	if c.closed.CompareAndSwap(false, true) {
		if c.transport != nil {
			c.transport.Close()
		}
		c.failPending("client closed")
	}
}
