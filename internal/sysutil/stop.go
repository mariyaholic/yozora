//go:build windows

package sysutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	procOpenProcess                = modKernel32.NewProc("OpenProcess")
	procTerminateProcess           = modKernel32.NewProc("TerminateProcess")
	procQueryFullProcessImageNameW = modKernel32.NewProc("QueryFullProcessImageNameW")
	procGetExitCodeProcess         = modKernel32.NewProc("GetExitCodeProcess")
)

const (
	processQueryLimitedInformation = 0x1000
	processTerminate               = 0x0001
	maxModuleFileNamePath          = 32768
	stillActive                    = 259
)

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

func processImageName(h uintptr) string {
	var buf [maxModuleFileNamePath]uint16
	sz := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(
		h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&sz)))
	if r == 0 || sz == 0 || sz > uint32(len(buf)) {
		return ""
	}
	return syscall.UTF16ToString(buf[:sz])
}

func pidImageName(pid int) string {
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)
	return processImageName(h)
}

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

func StopDaemon(dashboardJSONPath, expectedExe string) error {
	if strings.TrimSpace(expectedExe) == "" {
		return errors.New("sysutil: expected daemon path is empty")
	}
	pid, err := endpointPID(dashboardJSONPath)
	if err != nil {
		return err
	}
	guard, err := filepath.Abs(expectedExe)
	if err != nil {
		return fmt.Errorf("sysutil: resolve guard exe: %w", err)
	}
	guard = filepath.Clean(guard)

	h, _, _ := procOpenProcess.Call(processTerminate|processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return fmt.Errorf("sysutil: cannot open daemon pid %d", pid)
	}
	defer procCloseHandle.Call(h)

	name := processImageName(h)
	if name == "" {
		return fmt.Errorf("sysutil: cannot read image path for pid %d", pid)
	}
	if !strings.EqualFold(filepath.Clean(name), guard) {
		return fmt.Errorf("sysutil: refusing to stop pid %d (image %q does not match %q)", pid, name, guard)
	}

	var code uint32
	if r, _, _ := procGetExitCodeProcess.Call(h, uintptr(unsafe.Pointer(&code))); r == 0 {
		return fmt.Errorf("sysutil: cannot read exit status for pid %d", pid)
	}
	if code != stillActive {
		return fmt.Errorf("sysutil: daemon pid %d is no longer running", pid)
	}
	if r, _, _ := procTerminateProcess.Call(h, 1); r == 0 {
		return fmt.Errorf("sysutil: terminate daemon pid %d failed", pid)
	}
	return nil
}
