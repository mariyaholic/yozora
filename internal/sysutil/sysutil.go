//go:build windows

package sysutil

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	modAdvapi32   = syscall.NewLazyDLL("advapi32.dll")
	procRegOpen   = modAdvapi32.NewProc("RegOpenKeyExW")
	procRegCreate = modAdvapi32.NewProc("RegCreateKeyExW")
	procRegSet    = modAdvapi32.NewProc("RegSetValueExW")
	procRegQuery  = modAdvapi32.NewProc("RegQueryValueExW")
	procRegDel    = modAdvapi32.NewProc("RegDeleteValueW")
	procRegClose  = modAdvapi32.NewProc("RegCloseKey")

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

// acquireNamedMutex owns a named mutex for the life of the process. The returned
// release func closes the handle; ok is false when another owner already holds
// the name, which is a normal outcome rather than an error.
func acquireNamedMutex(name string) (bool, func(), error) {
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return false, func() {}, err
	}
	h, _, callErr := procCreateMut.Call(0, 0, uintptr(unsafe.Pointer(namePtr)))
	if h == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
			return false, func() {}, fmt.Errorf("sysutil: create mutex %s: %w", name, errno)
		}
		return false, func() {}, fmt.Errorf("sysutil: CreateMutexW %s returned a null handle", name)
	}
	if callErr == syscall.ERROR_ALREADY_EXISTS {
		procCloseHandle.Call(h)
		return false, func() {}, nil
	}
	return true, func() { procCloseHandle.Call(h) }, nil
}

// AcquireNamedMutex attempts to become the sole owner of the named mutex.
func AcquireNamedMutex(name string) (bool, func(), error) { return acquireNamedMutex(name) }

// AcquireSingleInstance acquires this package's own single-instance mutex.
func AcquireSingleInstance() (bool, func(), error) { return acquireNamedMutex(singleInstanceName) }

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

// runValueName stays "UikaResonance": installs from every earlier release
// registered under it, and the name is never shown to users (Task Manager's
// Startup tab shows the executable's own description), so a rename would only
// add a migration path that could leave a stale duplicate behind.
const runValueName = "UikaResonance"

const errorMoreData = 234

// readRunValue returns the command registered under name; ok is false when
// the value is absent or unreadable.
func readRunValue(name string) (string, bool) {
	h, err := runKeyPath()
	if err != nil {
		return "", false
	}
	defer procRegClose.Call(h)
	vn, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", false
	}
	buf := make([]uint16, 512)
	for range 2 {
		var typ uint32
		sz := uint32(len(buf) * 2)
		r1, _, _ := procRegQuery.Call(h, uintptr(unsafe.Pointer(vn)), 0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&sz)))
		switch r1 {
		case 0:
			n := min(int(sz/2), len(buf))
			return syscall.UTF16ToString(buf[:n]), true
		case errorMoreData:
			buf = make([]uint16, sz/2+1)
		default:
			return "", false
		}
	}
	return "", false
}

// createRunKey opens the Run key for writing, creating it first: a profile that
// has never had a startup entry (a fresh account, CI runners) has no Run key, and
// RegOpenKeyEx alone would make turning autostart on fail there.
func createRunKey() (uintptr, error) {
	var h uintptr
	path, err := syscall.UTF16PtrFromString(runKeySubkey)
	if err != nil {
		return 0, err
	}
	r1, _, _ := procRegCreate.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(path)), 0, 0, 0, keySetValue|keyQueryValue, 0, uintptr(unsafe.Pointer(&h)), 0)
	if r1 != 0 {
		return 0, fmt.Errorf("sysutil: create run key: %w", syscall.Errno(r1))
	}
	return h, nil
}

func writeRunValue(name, command string) error {
	h, err := createRunKey()
	if err != nil {
		return err
	}
	defer procRegClose.Call(h)
	vn, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	data, err := syscall.UTF16FromString(command)
	if err != nil {
		return err
	}
	r1, _, _ := procRegSet.Call(h, uintptr(unsafe.Pointer(vn)), 0, regSZ, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2))
	if r1 != 0 {
		return fmt.Errorf("sysutil: set run value: %w", syscall.Errno(r1))
	}
	return nil
}

// deleteRunValue succeeds when the key or value is already gone: turning
// autostart off has then nothing left to do.
func deleteRunValue(name string) error {
	h, err := runKeyPath()
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return err
	}
	defer procRegClose.Call(h)
	vn, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	r1, _, _ := procRegDel.Call(h, uintptr(unsafe.Pointer(vn)))
	if syscall.Errno(r1) == syscall.ERROR_FILE_NOT_FOUND {
		return nil
	}
	if r1 != 0 {
		return fmt.Errorf("sysutil: delete run value: %w", syscall.Errno(r1))
	}
	return nil
}

func InstallAutostart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return writeRunValue(runValueName, autostartCommand(exe))
}

func RemoveAutostart() error { return deleteRunValue(runValueName) }

func AutostartEnabled() bool {
	_, ok := readRunValue(runValueName)
	return ok
}

// RepairAutostart points an existing autostart entry at the running
// executable when it was registered from another path (a moved install, an
// old portable copy). It never turns autostart on; rewrote reports a change.
func RepairAutostart() (rewrote bool, err error) {
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	return repairAutostart(runValueName, exe, os.TempDir())
}

func repairAutostart(name, exe, tempDir string) (bool, error) {
	registered, ok := readRunValue(name)
	if !ok || !autostartNeedsRewrite(registered, exe, tempDir) {
		return false, nil
	}
	return true, writeRunValue(name, autostartCommand(exe))
}
