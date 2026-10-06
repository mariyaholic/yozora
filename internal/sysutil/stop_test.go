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

func TestStopDaemonRejectsStalePID(t *testing.T) {
	tmpJSON := filepath.Join(t.TempDir(), "dashboard.json")
	cmd := exec.Command(`C:\Windows\System32\cmd.exe`, "/c", "exit")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Wait()
	if err := os.WriteFile(tmpJSON, []byte(`{"pid":`+strconv.Itoa(pid)+`}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StopDaemon(tmpJSON, `C:\Windows\System32\cmd.exe`); err == nil {
		t.Fatal("StopDaemon did not reject a file pid no longer running")
	}
}

func TestStopDaemonRejectsMissingDashboardFile(t *testing.T) {
	if err := StopDaemon(filepath.Join(t.TempDir(), "missing.json"), `C:\Windows\System32\cmd.exe`); err == nil {
		t.Fatal("StopDaemon accepted a missing dashboard file")
	}
}

func TestStopDaemonRejectsMalformedPID(t *testing.T) {
	tmpJSON := filepath.Join(t.TempDir(), "dashboard.json")
	if err := os.WriteFile(tmpJSON, []byte(`{"pid":"not-a-number"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := StopDaemon(tmpJSON, `C:\Windows\System32\cmd.exe`); err == nil {
		t.Fatal("StopDaemon accepted a malformed PID value")
	}
}
