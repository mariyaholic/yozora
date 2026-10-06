//go:build windows

package smtc

import (
	"os"
	"runtime"
	"testing"
	"time"
)

// Opt-in leak probe (YOZORA_MEMLEAK_PROBE=1): polls the real SMTC manager
// repeatedly and reports Go heap growth. Read-only; no playback changes.
// Reveals whether the COM session walk leaks through Go's heap (visible)
// or only inside the COM apartment (invisible to Go's heap but visible in
// process private bytes).
func TestSMTCRepeatedPollMemory(t *testing.T) {
	if os.Getenv("YOZORA_MEMLEAK_PROBE") != "1" {
		t.Skip("opt-in via YOZORA_MEMLEAK_PROBE=1")
	}
	m, err := Connect()
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer m.Close()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := 0; i < 300; i++ {
		if _, err := m.Sessions(); err != nil {
			t.Fatalf("sessions: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	grew := float64(after.HeapAlloc)/1e6 - float64(before.HeapAlloc)/1e6
	t.Logf("Go heap delta after 300 polls: %.2f MB", grew)
	if grew > 20 {
		t.Fatalf("Go-side leak suspected: %.2f MB over 300 polls", grew)
	}
}
