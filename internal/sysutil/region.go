//go:build windows

package sysutil

import (
	"syscall"
	"unsafe"
)

// UserRegion is the Windows user's region as Windows names it: the geo name ("JP") where
// the system has one, else the region part of the locale name ("ja-JP"). It is "" when
// neither can be read; the caller decides what that means.
func UserRegion() string {
	if p := modKernel32.NewProc("GetUserDefaultGeoName"); p.Find() == nil {
		buf := make([]uint16, 16)
		if n, _, _ := p.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); n > 1 {
			return syscall.UTF16ToString(buf)
		}
	}
	if p := modKernel32.NewProc("GetUserDefaultLocaleName"); p.Find() == nil {
		buf := make([]uint16, 86) // LOCALE_NAME_MAX_LENGTH
		if n, _, _ := p.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); n > 1 {
			return syscall.UTF16ToString(buf)
		}
	}
	return ""
}
