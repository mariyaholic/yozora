//go:build windows

package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestFirstTrayQuitRequestRunsShutdownPath(t *testing.T) {
	dir := t.TempDir()
	a := &panelTrayActions{
		dashboardJSON: filepath.Join(dir, "missing-dashboard.json"),
		dataDir:       dir,
	}
	a.Quit()
	b, err := os.ReadFile(filepath.Join(dir, "launcher.log"))
	if err != nil {
		t.Fatalf("first quit request skipped shutdown path: %v", err)
	}
	if !strings.Contains(string(b), "tray quit: stop daemon:") {
		t.Fatalf("shutdown error not recorded: %s", b)
	}
}

func TestPanelCloseHidesAndQuitDestroys(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	class, _ := syscall.UTF16PtrFromString("Static")
	title, _ := syscall.UTF16PtrFromString("Yozora close-hook test")
	instance, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
	hwnd, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, 0, 0, instance, 0)
	if hwnd == 0 {
		t.Fatal("test window creation failed")
	}
	isWindow := syscall.NewLazyDLL("user32.dll").NewProc("IsWindow")
	isVisible := syscall.NewLazyDLL("user32.dll").NewProc("IsWindowVisible")
	sendMessage := syscall.NewLazyDLL("user32.dll").NewProc("SendMessageW")
	destroyWindow := syscall.NewLazyDLL("user32.dll").NewProc("DestroyWindow")
	defer func() {
		if r, _, _ := isWindow.Call(hwnd); r != 0 {
			destroyWindow.Call(hwnd)
		}
	}()

	a := &panelTrayActions{panelHwnd: hwnd}
	a.trayReady.Store(true)
	if err := installPanelWindowHook(hwnd, a); err != nil {
		t.Fatal(err)
	}
	procShowWindow.Call(hwnd, 5)
	sendMessage.Call(hwnd, 0x0010, 0, 0)
	if r, _, _ := isWindow.Call(hwnd); r == 0 {
		t.Fatal("panel close destroyed the window instead of hiding it")
	}
	if r, _, _ := isVisible.Call(hwnd); r != 0 {
		t.Fatal("panel window remained visible after close")
	}
	a.ShowPanel()
	if r, _, _ := isVisible.Call(hwnd); r == 0 {
		t.Fatal("tray open did not restore the hidden panel")
	}
	a.quitRequested.Store(true)
	sendMessage.Call(hwnd, 0x0010, 0, 0)
	if r, _, _ := isWindow.Call(hwnd); r != 0 {
		t.Fatal("quit close did not destroy the panel window")
	}
}
