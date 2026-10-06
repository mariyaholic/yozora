//go:build windows

package discordipc

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockDiscord struct {
	t                io.ReadWriteCloser
	gotActivity      chan Activity
	gotPong          chan []byte
	rejectActivity   bool
	echoButtonLabels bool
}

type blockingWriteTransport struct {
	closed chan struct{}
	once   sync.Once
}

func (b *blockingWriteTransport) Read([]byte) (int, error) {
	<-b.closed
	return 0, io.EOF
}
func (b *blockingWriteTransport) Write([]byte) (int, error) {
	<-b.closed
	return 0, io.ErrClosedPipe
}
func (b *blockingWriteTransport) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

type writeRecordingTransport struct {
	writes int
	data   bytes.Buffer
}

func (w *writeRecordingTransport) Read([]byte) (int, error) { return 0, io.EOF }
func (w *writeRecordingTransport) Write(p []byte) (int, error) {
	w.writes++
	return w.data.Write(p)
}
func (w *writeRecordingTransport) Close() error { return nil }

func (m *mockDiscord) serve() {
	go func() {
		for {
			var head [8]byte
			if _, err := io.ReadFull(m.t, head[:]); err != nil {
				return
			}
			op := int(binary.LittleEndian.Uint32(head[0:]))
			n := binary.LittleEndian.Uint32(head[4:])
			body := make([]byte, n)
			if n > 0 {
				if _, err := io.ReadFull(m.t, body); err != nil {
					return
				}
			}
			switch op {
			case opHandshake:
				m.reply(opFrame, map[string]any{"cmd": "DISPATCH", "evt": "READY", "data": map[string]any{"v": 1}})
			case opFrame:
				var f frame
				_ = json.Unmarshal(body, &f)
				if f.Cmd == "SET_ACTIVITY" {
					var args struct {
						PID      int      `json:"pid"`
						Activity Activity `json:"activity"`
					}
					_ = json.Unmarshal(f.Args, &args)
					m.gotActivity <- args.Activity
					if m.rejectActivity {
						m.reply(opFrame, map[string]any{
							"cmd": "SET_ACTIVITY", "evt": "ERROR", "nonce": f.Nonce,
							"data": map[string]any{"code": 4006, "message": "You must be authenticated to use this command"},
						})
					} else if m.echoButtonLabels {
						labels := make([]string, len(args.Activity.Buttons))
						urls := make([]string, len(args.Activity.Buttons))
						for i, button := range args.Activity.Buttons {
							labels[i] = button.Label
							urls[i] = button.URL
						}
						m.reply(opFrame, map[string]any{
							"cmd": "SET_ACTIVITY", "nonce": f.Nonce,
							"data": map[string]any{
								"name": args.Activity.Name, "details": args.Activity.Details,
								"buttons": labels, "metadata": map[string]any{"button_urls": urls},
							},
						})
					} else {
						m.reply(opFrame, map[string]any{"cmd": "SET_ACTIVITY", "nonce": f.Nonce, "data": args.Activity})
					}
				}
			case opPing:
				m.reply(opPong, map[string]any{"v": 1})
			case opPong:
				if m.gotPong != nil {
					m.gotPong <- append([]byte(nil), body...)
				}
			}
		}
	}()
}

func (m *mockDiscord) reply(op int, v any) {
	b, _ := json.Marshal(v)
	m.replyRaw(op, b)
}

func (m *mockDiscord) replyRaw(op int, b []byte) {
	packet := make([]byte, 8+len(b))
	binary.LittleEndian.PutUint32(packet[0:4], uint32(op))
	binary.LittleEndian.PutUint32(packet[4:8], uint32(len(b)))
	copy(packet[8:], b)
	_, _ = m.t.Write(packet)
}

func TestWriteRawSendsHeaderAndPayloadInSingleWrite(t *testing.T) {
	transport := &writeRecordingTransport{}
	client := NewClient(transport, "42")
	defer client.Close()
	payload := []byte(`{"x":1}`)
	if err := client.writeRaw(opFrame, payload); err != nil {
		t.Fatal(err)
	}
	if transport.writes != 1 {
		t.Fatalf("Write calls = %d, want 1 complete IPC frame", transport.writes)
	}
	want := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(want[0:4], uint32(opFrame))
	binary.LittleEndian.PutUint32(want[4:8], uint32(len(payload)))
	copy(want[8:], payload)
	if !bytes.Equal(transport.data.Bytes(), want) {
		t.Fatalf("written frame = %v, want %v", transport.data.Bytes(), want)
	}
}

func TestClientRepliesToServerPingAndRemainsUsable(t *testing.T) {
	a, b := net.Pipe()
	pingCh := make(chan []byte, 1)
	m := &mockDiscord{t: a, gotActivity: make(chan Activity, 1), gotPong: pingCh}
	m.serve()
	client := NewClient(b, "42")
	defer client.Close()
	if err := client.handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}

	pingPayload := []byte(`{"marker":"server-ping"}`)
	m.replyRaw(opPing, pingPayload)
	select {
	case pongPayload := <-pingCh:
		if !bytes.Equal(pongPayload, pingPayload) {
			t.Fatalf("PONG payload = %s, want echoed PING payload %s", pongPayload, pingPayload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("client did not answer server PING with PONG")
	}

	if err := client.SetActivity(&Activity{Name: "Yozora", Type: TypeListening}); err != nil {
		t.Fatalf("SET_ACTIVITY after PING: %v", err)
	}
}

func TestClientHandshakeAndSetActivity(t *testing.T) {
	a, b := net.Pipe()
	m := &mockDiscord{t: a, gotActivity: make(chan Activity, 4)}
	m.serve()

	c := NewClient(b, "123456789012345678")
	defer c.Close()

	if err := c.handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}

	act := &Activity{
		Name: "Yozora", Type: TypeListening,
		Details: "Way Back Into Love", State: "Hugh Grant",
		Timestamps: &Timestamps{Start: 1700000000, End: 1700000278},
		Assets:     &Assets{LargeImage: "https://example.test/art.jpg"},
		Buttons:    []Button{{Label: "Listen along", URL: "https://example.test"}},
		Instance:   true,
	}
	if err := c.SetActivity(act); err != nil {
		t.Fatalf("SetActivity: %v", err)
	}
	if got := c.AcceptedActivity(); got == nil || got.Details != act.Details || got.Name != act.Name {
		t.Fatalf("accepted Discord activity = %+v, want published payload", got)
	}
	select {
	case got := <-m.gotActivity:
		if got.Type != TypeListening || got.Details != "Way Back Into Love" {
			t.Fatalf("server got wrong activity: %+v", got)
		}
		if len(got.Buttons) != 1 || got.Buttons[0].Label != "Listen along" {
			t.Fatalf("buttons: %+v", got.Buttons)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no activity received")
	}

	if err := c.SetActivity(nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if err := c.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestSetActivityPreservesLabelOnlyButtonReadback(t *testing.T) {
	a, b := net.Pipe()
	m := &mockDiscord{t: a, gotActivity: make(chan Activity, 1), echoButtonLabels: true}
	m.serve()
	c := NewClient(b, "42")
	defer c.Close()
	if err := c.handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	activity := &Activity{
		Name: "Spotify", Details: "fixture track",
		Buttons: []Button{
			{Label: "Listen on Spotify", URL: "https://open.spotify.com/track/fixture"},
			{Label: "Yozora", URL: "https://example.test/yozora"},
		},
	}
	if err := c.SetActivity(activity); err != nil {
		t.Fatalf("label-only readback must not reject an accepted activity: %v", err)
	}
	var readback struct {
		Name     string   `json:"name"`
		Details  string   `json:"details"`
		Buttons  []string `json:"buttons"`
		Metadata struct {
			ButtonURLs []string `json:"button_urls"`
		} `json:"metadata"`
	}
	payload := c.AcceptedPayload()
	if err := json.Unmarshal(payload, &readback); err != nil {
		t.Fatalf("decode raw readback: %v", err)
	}
	if readback.Name != activity.Name || readback.Details != activity.Details {
		t.Fatalf("readback lost activity text: %+v", readback)
	}
	if len(readback.Buttons) != len(activity.Buttons) || len(readback.Metadata.ButtonURLs) != len(activity.Buttons) {
		t.Fatalf("readback lost labels or metadata URLs: %+v", readback)
	}
	for i, button := range activity.Buttons {
		if readback.Buttons[i] != button.Label || readback.Metadata.ButtonURLs[i] != button.URL {
			t.Errorf("readback button %d = (%q, %q), want (%q, %q)", i, readback.Buttons[i], readback.Metadata.ButtonURLs[i], button.Label, button.URL)
		}
	}
	if got := c.AcceptedActivity(); got != nil {
		t.Fatalf("incompatible optional typed readback = %+v, want nil", got)
	}
	payload[0] = 'X'
	if got := c.AcceptedPayload(); !json.Valid(got) {
		t.Fatalf("caller mutation corrupted retained readback: %s", got)
	}
}

func TestClearSendsNullActivity(t *testing.T) {
	a, b := net.Pipe()
	m := &mockDiscord{t: a, gotActivity: make(chan Activity, 4)}
	m.serve()
	c := NewClient(b, "42")
	defer c.Close()
	_ = c.handshake()
	_ = c.SetActivity(nil)
}

func TestClientMarksTransportDead(t *testing.T) {
	a, b := net.Pipe()
	m := &mockDiscord{t: a, gotActivity: make(chan Activity, 1)}
	m.serve()
	c := NewClient(b, "42")
	defer c.Close()
	if err := c.handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}

	_ = a.Close()
	deadline := time.Now().Add(time.Second)
	for c.Alive() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if c.Alive() {
		t.Fatal("client still reports alive after transport closed")
	}
}

func TestWriteRawTimesOutAndClosesBlockedTransport(t *testing.T) {
	transport := &blockingWriteTransport{closed: make(chan struct{})}
	client := NewClient(transport, "42")
	start := time.Now()
	err := client.writeRawTimeout(opFrame, []byte(`{"cmd":"SET_ACTIVITY"}`), 10*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "write timed out") {
		t.Fatalf("write error = %v, want timeout", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("blocked write returned after %s, want prompt timeout", elapsed)
	}
	if client.Alive() {
		t.Fatal("client remains alive after timed-out write")
	}
}

func TestSetActivityReturnsDiscordErrorEvent(t *testing.T) {
	a, b := net.Pipe()
	m := &mockDiscord{t: a, gotActivity: make(chan Activity, 1), rejectActivity: true}
	m.serve()
	c := NewClient(b, "42")
	defer c.Close()
	if err := c.handshake(); err != nil {
		t.Fatalf("handshake: %v", err)
	}

	err := c.SetActivity(&Activity{Details: "current track"})
	if err == nil {
		t.Fatal("SetActivity returned nil for Discord ERROR event")
	}
	for _, want := range []string{"code=4006", "must be authenticated"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("SetActivity error = %q, want substring %q", err, want)
		}
	}
}
