//go:build windows

package browserwin

import (
	"sync"
	"syscall"
	"unsafe"
)

type Window struct {
	HWND  uintptr
	Title string
}

var (
	user32        = syscall.NewLazyDLL("user32.dll")
	enumWindows   = user32.NewProc("EnumWindows")
	isVisible     = user32.NewProc("IsWindowVisible")
	getClassName  = user32.NewProc("GetClassNameW")
	getTextLength = user32.NewProc("GetWindowTextLengthW")
	getText       = user32.NewProc("GetWindowTextW")
	enumMu        sync.Mutex
	enumerated    []Window
	// NewCallback allocates permanently: create it once, never once per scan.
	enumCallback = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		visible, _, _ := isVisible.Call(hwnd)
		if visible == 0 {
			return 1
		}
		var class [256]uint16
		n, _, _ := getClassName.Call(hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
		if n == 0 || syscall.UTF16ToString(class[:]) != "Chrome_WidgetWin_1" {
			return 1
		}
		length, _, _ := getTextLength.Call(hwnd)
		if length == 0 || length > 1024 {
			return 1
		}
		// Extra sentinel space detects growth between the length and text calls.
		var title [1026]uint16
		n, _, _ = getText.Call(hwnd, uintptr(unsafe.Pointer(&title[0])), uintptr(len(title)))
		if n == 0 || n > 1024 {
			return 1
		}
		enumerated = append(enumerated, Window{hwnd, syscall.UTF16ToString(title[:n])})
		return 1
	})
)

// Enumerate scans visible top-level Chromium windows. Process-image verification
// is intentionally omitted; classification still requires an exact title match.
func Enumerate() []Window {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumerated = nil
	enumWindows.Call(enumCallback, 0)
	result := enumerated
	enumerated = nil
	return result
}
