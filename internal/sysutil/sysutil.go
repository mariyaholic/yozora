//go:build windows

// Package sysutil groups small Windows integrations: autostart registry
// entry and the single-instance mutex.
package sysutil

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	modAdvapi32  = syscall.NewLazyDLL("advapi32.dll")
	procRegOpen  = modAdvapi32.NewProc("RegOpenKeyExW")
	procRegSet   = modAdvapi32.NewProc("RegSetValueExW")
	procRegQuery = modAdvapi32.NewProc("RegQueryValueExW")
	procRegDel   = modAdvapi32.NewProc("RegDeleteValueW")
	procRegClose = modAdvapi32.NewProc("RegCloseKey")

	modKernel32     = syscall.NewLazyDLL("kernel32.dll")
	procCreateMut   = modKernel32.NewProc("CreateMutexW")
	procCloseHandle = modKernel32.NewProc("CloseHandle")
)

const (
	hkeyCurrentUser    = 0x80000001
	keySetValue        = 0x0002
	keyQueryValue      = 0x0001
	regSZ              = 1
	singleInstanceName = `Local\uika-resonance-singleton`
	runKeySubkey       = `Software\Microsoft\Windows\CurrentVersion\Run`
)

// AcquireSingleInstance tries to create the process mutex. Returns false
// when another instance is running. The returned release function closes the
// handle owned by this process.
func AcquireSingleInstance() (bool, func(), error) {
	name, err := syscall.UTF16PtrFromString(singleInstanceName)
	if err != nil {
		return false, func() {}, err
	}
	h, _, callErr := procCreateMut.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
			return false, func() {}, fmt.Errorf("sysutil: create mutex: %w", errno)
		}
		return false, func() {}, fmt.Errorf("sysutil: CreateMutexW returned a null handle")
	}
	if callErr == syscall.ERROR_ALREADY_EXISTS {
		procCloseHandle.Call(h)
		return false, func() {}, nil
	}
	return true, func() { procCloseHandle.Call(h) }, nil
}

func runKeyPath() (uintptr, error) {
	var h uintptr
	path, err := syscall.UTF16PtrFromString(runKeySubkey)
	if err != nil {
		return 0, err
	}
	r1, _, _ := procRegOpen.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(path)), 0, keySetValue|keyQueryValue, uintptr(unsafe.Pointer(&h)))
	if r1 != 0 {
		return 0, fmt.Errorf("sysutil: open run key: %w", syscall.Errno(r1))
	}
	return h, nil
}

func valueName() (*uint16, error) { return syscall.UTF16PtrFromString("UikaResonance") }

// InstallAutostart adds the HKCU Run entry for the current executable.
func InstallAutostart() error {
	h, err := runKeyPath()
	if err != nil {
		return err
	}
	defer procRegClose.Call(h)
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	vn, _ := valueName()
	data, _ := syscall.UTF16FromString(`"` + exe + `" serve`)
	r1, _, _ := procRegSet.Call(h, uintptr(unsafe.Pointer(vn)), 0, regSZ, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2))
	if r1 != 0 {
		return fmt.Errorf("sysutil: set run value: %w", syscall.Errno(r1))
	}
	return nil
}

// RemoveAutostart deletes the HKCU Run entry.
func RemoveAutostart() error {
	h, err := runKeyPath()
	if err != nil {
		return err
	}
	defer procRegClose.Call(h)
	vn, _ := valueName()
	r1, _, _ := procRegDel.Call(h, uintptr(unsafe.Pointer(vn)))
	if r1 != 0 {
		return fmt.Errorf("sysutil: delete run value: %w", syscall.Errno(r1))
	}
	return nil
}

// AutostartEnabled reports whether the Run entry exists.
func AutostartEnabled() bool {
	h, err := runKeyPath()
	if err != nil {
		return false
	}
	defer procRegClose.Call(h)
	vn, _ := valueName()
	var typ uint32
	var buf [512]uint16
	var sz uint32 = 1024
	r1, _, _ := procRegQuery.Call(h, uintptr(unsafe.Pointer(vn)), 0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&sz)))
	return r1 == 0
}
