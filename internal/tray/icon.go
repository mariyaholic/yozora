//go:build windows

package tray

import (
	_ "embed"
	"encoding/binary"
	"unsafe"
)

//go:embed yozora-tray.ico
var trayICO []byte

var (
	procGetSystemMetrics        = modUser32.NewProc("GetSystemMetrics")
	procCreateIconFromResourceE = modUser32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon             = modUser32.NewProc("DestroyIcon")
	procLoadIconW               = modUser32.NewProc("LoadIconW")
	procRegisterWindowMessageW  = modUser32.NewProc("RegisterWindowMessageW")
)

const (
	smCxSmIcon     = 49
	idiApplication = 32512
)

func icoEntry(ico []byte, size int) []byte {
	if len(ico) < 6 {
		return nil
	}
	count := int(binary.LittleEndian.Uint16(ico[4:6]))
	var best []byte
	bestW := 0
	for i := 0; i < count; i++ {
		e := 6 + 16*i
		if e+16 > len(ico) {
			break
		}
		w := int(ico[e])
		if w == 0 {
			w = 256
		}
		n := int(binary.LittleEndian.Uint32(ico[e+8:]))
		off := int(binary.LittleEndian.Uint32(ico[e+12:]))
		if off < 0 || n <= 0 || off+n > len(ico) {
			continue
		}
		better := best == nil ||
			(bestW < size && w > bestW) ||
			(w >= size && w < bestW)
		if better {
			best, bestW = ico[off:off+n], w
		}
	}
	return best
}

func loadTrayIcon() uintptr {
	size, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
	if size == 0 {
		size = 16
	}
	if img := icoEntry(trayICO, int(size)); img != nil {
		icon, _, _ := procCreateIconFromResourceE.Call(
			uintptr(unsafe.Pointer(&img[0])), uintptr(len(img)), 1, 0x00030000, size, size, 0)
		if icon != 0 {
			return icon
		}
	}
	icon, _, _ := procLoadImageW.Call(moduleHandle(), iconResource, imageIcon, size, size, 0)
	if icon != 0 {
		return icon
	}
	icon, _, _ = procLoadIconW.Call(0, idiApplication)
	return icon
}
