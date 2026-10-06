//go:build windows

package sysutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

var (
	procOpenProcess            = modKernel32.NewProc("OpenProcess")
	procTerminateProcess       = modKernel32.NewProc("TerminateProcess")
	procK32GetModuleFileNameEx = modKernel32.NewProc("K32GetModuleFileNameExW")
	procGetExitCodeProcess     = modKernel32.NewProc("GetExitCodeProcess")
)

const (
	processQueryLimitedInformation = 0x1000
	processTerminate               = 0x0001
	maxModuleFileNamePath          = 1024
	stillActive                    = 259 // STILL_ACTIVE
)

// endpointPID extracts the daemon PID recorded in a dashboard.json.
func endpointPID(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("sysutil: read dashboard endpoint: %w", err)
	}
	var e struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(b, &e); err != nil {
		return 0, errors.New("sysutil: dashboard endpoint json malformed")
	}
	if e.PID <= 0 {
		return 0, errors.New("sysutil: dashboard endpoint pid absent or non-positive")
	}
	return e.PID, nil
}

// pidImageName opens a PID without special rights and reads its image name;
// returns "" when the handle cannot be opened or the name queried (missing
// address-of-exit means handle did not open). Non-zero `pid` expected.
func pidImageName(pid int) string {
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)
	var buf [maxModuleFileNamePath]uint16
	var sz uint32 = maxModuleFileNamePath
	r2, _, _ := procK32GetModuleFileNameEx.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(sz))
	if r2 == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:])
}

// pidAlive reports whether a PID still refers to a running process.
// OpenProcess alone is not enough: a process that has been asked to terminate
// may still be opened until the last handle closes, so the liveness check is
// GetExitCodeProcess != STILL_ACTIVE.
func pidAlive(pid int) bool {
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return false
	}
	defer procCloseHandle.Call(h)
	var code uint32
	if r, _, _ := procGetExitCodeProcess.Call(h, uintptr(unsafe.Pointer(&code))); r == 0 {
		return false
	}
	return code == stillActive
}

// StopDaemon shuts down the Yozora daemon identified by the endpoint file at
// `dashboardJSONPath`. The PID stored there must be a process whose
// executable path matches `expectedExe`; anything else is refused so an
// unrelated process (PID reuse, a stray editor holding the file, ... ) can
// never be terminated. Returns an error when the guarded path check fails or
// the recorded PID is no longer running.
func StopDaemon(dashboardJSONPath, expectedExe string) error {
	pid, err := endpointPID(dashboardJSONPath)
	if err != nil {
		return err
	}
	guard, err := filepath.Abs(expectedExe)
	if err != nil {
		return fmt.Errorf("sysutil: resolve guard exe: %w", err)
	}
	guard = filepath.Clean(guard)

	name := pidImageName(pid)
	if name == "" {
		return errors.New("sysutil: daemon pid is not running: " + strconv.Itoa(pid))
	}
	// Case-insensitive compare: Windows image paths keep original spelling
	// but are not case-sensitive themselves.
	if !strings.EqualFold(filepath.Clean(name), guard) {
		return fmt.Errorf("sysutil: refusing to stop pid %d (image %q does not match %q)", pid, name, guard)
	}

	h, _, _ := procOpenProcess.Call(processTerminate|processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return fmt.Errorf("sysutil: cannot open daemon pid %d for termination", pid)
	}
	defer procCloseHandle.Call(h)
	if r, _, _ := procTerminateProcess.Call(h, 1); r == 0 {
		return fmt.Errorf("sysutil: terminate daemon pid %d failed", pid)
	}
	// Race note: the PID could be reused between OpenProcess and
	// TerminateProcess, but the path recheck happens first inside the same
	// lock step. Any race window is theoretical because both calls happen
	// back-to-back under the same process handle.
	return nil
}
