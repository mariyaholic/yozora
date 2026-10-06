//go:build windows

package discordipc

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
)

func TestNamedPipePublishesWhileReaderIsPending(t *testing.T) {
	path := fmt.Sprintf(`\\.\pipe\yozora-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	listener, err := winio.ListenPipe(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			accepted <- err
			return
		}
		server := &mockDiscord{t: conn, gotActivity: make(chan Activity, 2)}
		t.Cleanup(func() { _ = conn.Close() })
		server.serve()
		accepted <- nil
	}()

	var transport Transport
	deadline := time.Now().Add(time.Second)
	for {
		transport, err = dialPipe(path)
		if err == nil || time.Now().After(deadline) {
			break
		}

		time.Sleep(5 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(transport, "42")
	defer client.Close()
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
	if err := client.handshake(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	if err := client.SetActivity(&Activity{Details: "private fixture track"}); err != nil {
		t.Fatalf("publish while reader pending: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("local pipe publish took %s, want under one second", elapsed)
	}
}
