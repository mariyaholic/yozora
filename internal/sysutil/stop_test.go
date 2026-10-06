//go:build windows

package sysutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// waitGone polls until the process exits or the deadline passes.
func waitGone(t *testing.T, pid int) bool {
	t.Helper()
	for i := 30; i > 0; i-- {
		if !pidAlive(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// TestStopDaemonKillsGuardedPathPID spawns a live cmd.exe, records its real
// PID in a dashboard.json, and checks StopDaemon terminates it because the
// image path matches the guarded expectation.
func TestStopDaemonKillsGuardedPathPID(t *testing.T) {
	tmpJSON := filepath.Join(t.TempDir(), "dashboard.json")
	exe := `C:\Windows\System32\cmd.exe`
	cmd := exec.Command(exe, "/c", "ping", "127.0.0.1", "-n", "60", ">NUL")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	if err := os.WriteFile(tmpJSON, []byte(`{"pid":`+strconv.Itoa(pid)+`}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StopDaemon(tmpJSON, exe); err != nil {
		t.Fatalf("StopDaemon returned error: %v", err)
	}
	if !waitGone(t, pid) {
		t.Fatalf("daemon pid %d was not terminated", pid)
	}
}

// TestStopDaemonRejectsOtherPathPID refuses to kill something that is not the
// daemon recognized by the expected path.
func TestStopDaemonRejectsOtherPathPID(t *testing.T) {
	tmpJSON := filepath.Join(t.TempDir(), "dashboard.json")
	cmd := exec.Command("C:\\Windows\\System32\\cmd.exe", "/c", "ping", "127.0.0.1", "-n", "60", ">NUL")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	if err := os.WriteFile(tmpJSON, []byte(`{"pid":`+strconv.Itoa(pid)+`}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StopDaemon(tmpJSON, `C:\definitely-not-cmd.exe`); err == nil {
		t.Fatal("StopDaemon accepted a different executable path")
	}
}

// TestStopDaemonRejectsStalePID writes a PID whose process already exited.
// StopDaemon must report the stale entry, never kill a reclaimed PID blindly.
func TestStopDaemonRejectsStalePID(t *testing.T) {
	tmpJSON := filepath.Join(t.TempDir(), "dashboard.json")
	cmd := exec.Command(`C:\Windows\System32\cmd.exe`, "/c", "exit")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Wait() // now gone
	if err := os.WriteFile(tmpJSON, []byte(`{"pid":`+strconv.Itoa(pid)+`}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StopDaemon(tmpJSON, `C:\Windows\System32\cmd.exe`); err == nil {
		t.Fatal("StopDaemon did not reject a file pid no longer running")
	}
}

// TestStopDaemonRejectsMissingDashboardFile confirms the guarded-stop refuses
// when dashboard.json is absent.
func TestStopDaemonRejectsMissingDashboardFile(t *testing.T) {
	if err := StopDaemon(filepath.Join(t.TempDir(), "missing.json"), `C:\Windows\System32\cmd.exe`); err == nil {
		t.Fatal("StopDaemon accepted a missing dashboard file")
	}
}

// TestStopDaemonRejectsMalformedPID records a non-numeric PID value; the
// guarded stop shall not fall through to an unguarded termination.
func TestStopDaemonRejectsMalformedPID(t *testing.T) {
	tmpJSON := filepath.Join(t.TempDir(), "dashboard.json")
	if err := os.WriteFile(tmpJSON, []byte(`{"pid":"not-a-number"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StopDaemon(tmpJSON, `C:\Windows\System32\cmd.exe`); err == nil {
		t.Fatal("StopDaemon accepted a malformed PID value")
	}
}
