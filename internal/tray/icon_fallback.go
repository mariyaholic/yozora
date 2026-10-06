//go:build windows

package tray

import (
	"os"
	"syscall"
	"unsafe"
)

// loadIcon loads the Yozora icon for the tray: resource id 1 from this
// process's module first; when that fails, through a read-only data-file
// view of the same executable (identical bytes, serviced differently).
func (p *Panel) loadIcon() uintptr {
	if h, ok := loadIconFromModule(moduleHandle(), iconResource); ok {
		return h
	}
	self, err := os.Executable()
	if err != nil {
		return 0
	}
	mod := loadDataModule(self)
	if mod == 0 {
		return 0
	}
	p.dataModule = mod
	h, _ := loadIconFromModule(mod, iconResource)
	return h
}

// loadIconFromModule tries the resource-flag ladder: LR_SHARED fails on
// data-file modules (shared handles need a true module context), so the
// default-size load comes second.
func loadIconFromModule(mod, id uintptr) (uintptr, bool) {
	h, _, _ := procLoadImageW.Call(mod, id, imageIcon, 0, 0, lrShared)
	if h != 0 {
		return h, true
	}
	h, _, _ = procLoadImageW.Call(mod, id, imageIcon, 0, 0, lrDefaultSize)
	if h != 0 {
		return h, true
	}
	return 0, false
}

const lrDefaultSize = 0x00000040

// loadDataModule opens the executable for resource reading only.
func loadDataModule(path string) uintptr {
	ntf, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	h, _, _ := procLoadLibraryExW.Call(uintptr(unsafe.Pointer(ntf)), 0, 0x2|0x20) // DATAFILE|IMAGE_RESOURCE
	return h
}

var procLoadLibraryExW = modKernel32.NewProc("LoadLibraryExW")
