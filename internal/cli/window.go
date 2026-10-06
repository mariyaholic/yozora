//go:build windows

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	webview "github.com/jchv/go-webview2"
)

const (
	notifyInfo  = 0x40 // MB_ICONINFORMATION
	notifyError = 0x10 // MB_ICONERROR
)

var (
	procMessageBoxW      = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
	procShowWindow       = syscall.NewLazyDLL("user32.dll").NewProc("ShowWindow")
	procGetDpiForSystem  = syscall.NewLazyDLL("user32.dll").NewProc("GetDpiForSystem")
	procGetConsoleWindow = syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleWindow")
	procFindWindowW      = syscall.NewLazyDLL("user32.dll").NewProc("FindWindowW")
	procCreateWindowExW  = syscall.NewLazyDLL("user32.dll").NewProc("CreateWindowExW")
	procSetPropW         = syscall.NewLazyDLL("user32.dll").NewProc("SetPropW")
	procGetPropW         = syscall.NewLazyDLL("user32.dll").NewProc("GetPropW")
	procForeground       = syscall.NewLazyDLL("user32.dll").NewProc("SetForegroundWindow")
	procSetCursorPos     = syscall.NewLazyDLL("user32.dll").NewProc("SetCursorPos")
)

const (
	panelEntryClass = "Static" // built-in user32 class, safe for message-only
	panelEntryTitle = "YozoraControlPanelEntry"
	panelHWNDProp   = "YozoraPanelHWND"
	hwndMessage     = ^uintptr(2) // (HWND)-3
)

// createPanelEntry makes a hidden message-only window and stores the real
// panel HWND in a property, so a second launcher can lift the panel later.
func createPanelEntry(panelHwnd uintptr) {
	title, _ := syscall.UTF16PtrFromString(panelEntryTitle)
	class, _ := syscall.UTF16PtrFromString(panelEntryClass)
	entry, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, hwndMessage, 0, 0, 0)
	if entry != 0 {
		prop, _ := syscall.UTF16PtrFromString(panelHWNDProp)
		procSetPropW.Call(entry, uintptr(unsafe.Pointer(prop)), panelHwnd)
	}
}

// activateRunningPanel focuses the panel window owned by the first launcher.
func activateRunningPanel() int {
	title, _ := syscall.UTF16PtrFromString(panelEntryTitle)
	class, _ := syscall.UTF16PtrFromString(panelEntryClass)
	entry, _, _ := procFindWindowW.Call(
		uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)))
	if entry == 0 {
		// The owner may still be initializing; the fresh window will appear
		// momentarily, so this press simply exits quietly.
		return 0
	}
	prop, _ := syscall.UTF16PtrFromString(panelHWNDProp)
	panel, _, _ := procGetPropW.Call(entry, uintptr(unsafe.Pointer(prop)))
	if panel == 0 {
		return 0
	}
	// A synthetic input event grants this process foreground rights.
	procSetCursorPos.Call(0, 0)
	procSetCursorPos.Call(1, 1)
	procShowWindow.Call(panel, 9 /* SW_RESTORE */)
	procForeground.Call(panel)
	return 0
}

// Control-panel window size in CSS pixels (matches the dashboard's 560px
// column). Sizes are scaled by the system DPI so the layout gets the same
// room on 100% and 125% displays.
const (
	windowCSSWidth  = 560
	windowCSSHeight = 760
)

func scaledWindowSize() (uint, uint) {
	dpi, _, _ := procGetDpiForSystem.Call()
	if dpi == 0 {
		dpi = 96
	}
	return uint(windowCSSWidth * int(dpi) / 96), uint(windowCSSHeight * int(dpi) / 96)
}

// hideLauncherConsole keeps double-click launches from flashing a console
// window. Passthrough commands (`Yozora.exe doctor` ...) never reach this.
func hideLauncherConsole() {
	console, _, _ := procGetConsoleWindow.Call()
	if console != 0 {
		procShowWindow.Call(console, 0 /* SW_HIDE */)
	}
}

func launcherNotify(title, message string, flags uintptr) {
	t, errT := syscall.UTF16PtrFromString(title)
	m, errM := syscall.UTF16PtrFromString(message)
	if errT != nil || errM != nil {
		return
	}
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags)
}

// openDashboardWindow shows the control panel in a native WebView2 window —
// pure Go, no browser tab, no Electron/Node. Returns false when a window
// cannot be created so the caller can fall back to the default browser.
func openDashboardWindow(endpoint dashboardEndpoint, dataDir string) (ok bool) {
	u, err := dashboardURL(endpoint)
	if err != nil {
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			launcherLog(dataDir, fmt.Sprintf("webview panic: %v", r))
			ok = false
		}
	}()
	width, height := scaledWindowSize()
	w := webview.NewWithOptions(webview.WebViewOptions{
		AutoFocus: true,
		DataPath:  filepath.Join(dataDir, "webview"),
		WindowOptions: webview.WindowOptions{
			Title:  "Yozora",
			Width:  width,
			Height: height,
			IconId: 1, // rsrc icon generated from assets/yozora.ico (go-winres)
			Center: true,
		},
	})
	if w == nil {
		launcherLog(dataDir, "webview unavailable; falling back to browser")
		return false
	}
	createPanelEntry(uintptr(w.Window()))
	defer w.Destroy()
	w.Navigate(u.String())
	w.Run()
	return true
}

func launcherLog(dataDir, line string) {
	f, err := os.OpenFile(filepath.Join(dataDir, "launcher.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), line)
}
