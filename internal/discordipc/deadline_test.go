//go:build windows

package discordipc

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// deadlineTransport records whether a deadline was applied and fails writes the
// way a real pipe does once a write deadline has expired. It lets the deadline
// branch be asserted directly, without depending on timing.
type deadlineTransport struct {
	mu            sync.Mutex
	writeDeadline bool
	readDeadline  bool
	closed        bool
	writeErr      error
}

func (d *deadlineTransport) Read([]byte) (int, error) { return 0, os.ErrDeadlineExceeded }

func (d *deadlineTransport) Write([]byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return 0, d.writeErr
}

func (d *deadlineTransport) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

func (d *deadlineTransport) SetWriteDeadline(time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writeDeadline = true
	return nil
}

func (d *deadlineTransport) SetReadDeadline(time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.readDeadline = true
	return nil
}

func (d *deadlineTransport) state() (wrote, closed bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.writeDeadline, d.closed
}

func TestWriteRawTimeoutUsesDeadlineOnDeadlineCapableTransport(t *testing.T) {
	transport := &deadlineTransport{writeErr: os.ErrDeadlineExceeded}
	client := NewClient(transport, "42")

	start := time.Now()
	err := client.writeRawTimeout(opFrame, []byte(`{"cmd":"SET_ACTIVITY"}`), 50*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil || !strings.Contains(err.Error(), "write timed out") {
		t.Fatalf("write error = %v, want a timeout", err)
	}
	if elapsed > time.Second {
		t.Fatalf("deadline write returned after %s, want immediately", elapsed)
	}
	wrote, closed := transport.state()
	if !wrote {
		t.Fatal("client did not set a write deadline on a deadline-capable transport")
	}
	if !closed || client.Alive() {
		t.Fatal("client must close after a timed-out write")
	}
}

func TestWriteRawReturnsNonTimeoutErrorWithoutClosing(t *testing.T) {
	sentinel := errors.New("pipe gone")
	transport := &deadlineTransport{writeErr: sentinel}
	client := NewClient(transport, "42")
	defer client.Close()

	if err := client.writeRawTimeout(opFrame, []byte(`{}`), 50*time.Millisecond); !errors.Is(err, sentinel) {
		t.Fatalf("write error = %v, want the underlying transport error", err)
	}
	if !client.Alive() {
		t.Fatal("a non-timeout write error must not close the client")
	}
}
