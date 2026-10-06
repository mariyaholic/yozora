//go:build windows

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	webview "github.com/jchv/go-webview2"

	"uika-resonance/internal/sysutil"
	"uika-resonance/internal/tray"
)

const (
	notifyInfo  = 0x40
	notifyError = 0x10
)

var (
	procMessageBoxW       = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
	procShowWindow        = syscall.NewLazyDLL("user32.dll").NewProc("ShowWindow")
	procGetDpiForSystem   = syscall.NewLazyDLL("user32.dll").NewProc("GetDpiForSystem")
	procGetConsoleWindow  = syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleWindow")
	procCreateWindowExW   = syscall.NewLazyDLL("user32.dll").NewProc("CreateWindowExW")
	procSetPropW          = syscall.NewLazyDLL("user32.dll").NewProc("SetPropW")
	procGetPropW          = syscall.NewLazyDLL("user32.dll").NewProc("GetPropW")
	procFindWindowExW     = syscall.NewLazyDLL("user32.dll").NewProc("FindWindowExW")
	procForeground        = syscall.NewLazyDLL("user32.dll").NewProc("SetForegroundWindow")
	procSetWindowLongPtrW = syscall.NewLazyDLL("user32.dll").NewProc("SetWindowLongPtrW")
	procCallWindowProcW   = syscall.NewLazyDLL("user32.dll").NewProc("CallWindowProcW")
)

const (
	panelEntryClass = "Static"
	panelEntryTitle = "YozoraControlPanelEntry"
	panelHWNDProp   = "YozoraPanelHWND"
	hwndMessage     = ^uintptr(2)
)

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

func activateRunningPanel() int {
	title, _ := syscall.UTF16PtrFromString(panelEntryTitle)
	class, _ := syscall.UTF16PtrFromString(panelEntryClass)
	entry, _, _ := procFindWindowExW.Call(hwndMessage, 0,
		uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)))
	if entry == 0 {

		return 0
	}
	prop, _ := syscall.UTF16PtrFromString(panelHWNDProp)
	panel, _, _ := procGetPropW.Call(entry, uintptr(unsafe.Pointer(prop)))
	if panel == 0 {
		return 0
	}
	procShowWindow.Call(panel, 9)
	procForeground.Call(panel)
	return 0
}

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

func hideLauncherConsole() {
	console, _, _ := procGetConsoleWindow.Call()
	if console != 0 {
		procShowWindow.Call(console, 0)
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

func openDashboardWindow(endpoint dashboardEndpoint, dataDir string) (ok bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
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
			IconId: 1,
			Center: true,
		},
	})
	if w == nil {
		launcherLog(dataDir, "webview unavailable; falling back to browser")
		return false
	}
	actions := &panelTrayActions{
		panel:         w,
		panelHwnd:     uintptr(w.Window()),
		dashboardJSON: dashboardEndpointPath(dataDir),
		daemonExe:     daemonExecutableOrEmpty(dataDir),
		dataDir:       dataDir,
	}
	createPanelEntry(actions.panelHwnd)
	if err := installPanelWindowHook(actions.panelHwnd, actions); err != nil {
		launcherLog(dataDir, "panel close hook: "+err.Error())
	}
	trayPanel := tray.New(actions)
	trayDone := make(chan struct{})
	go func() {
		defer close(trayDone)
		if err := trayPanel.Run(); err != nil {
			actions.trayReady.Store(false)
			launcherLog(dataDir, err.Error())
		}
	}()
	defer func() {

		trayPanel.PostClose()
		select {
		case <-trayDone:
		case <-time.After(2 * time.Second):
		}
	}()

	w.Navigate(u.String())
	w.Run()
	<-trayDone
	return true
}

type panelTrayActions struct {
	panel         webview.WebView
	panelHwnd     uintptr
	dashboardJSON string
	daemonExe     string
	dataDir       string
	trayReady     atomic.Bool
	quitRequested atomic.Bool
}

func (a *panelTrayActions) ShowPanel() {
	if a.panelHwnd == 0 {
		return
	}
	procShowWindow.Call(a.panelHwnd, 9)
	procForeground.Call(a.panelHwnd)
}

func (a *panelTrayActions) TrayReady() {
	a.trayReady.Store(true)
}

func (a *panelTrayActions) Quit() {
	if !a.quitRequested.CompareAndSwap(false, true) {
		return
	}
	if err := sysutil.StopDaemon(a.dashboardJSON, a.daemonExe); err != nil {
		launcherLog(a.dataDir, "tray quit: stop daemon: "+err.Error())
	}
	if a.panel != nil {
		a.panel.Destroy()
	}
}

type panelWindowHook struct {
	original uintptr
	actions  *panelTrayActions
}

var panelWindowHooks = struct {
	sync.RWMutex
	windows map[uintptr]panelWindowHook
}{windows: make(map[uintptr]panelWindowHook)}

var panelWindowProcCallback = syscall.NewCallback(panelWindowProc)

func installPanelWindowHook(hwnd uintptr, actions *panelTrayActions) error {
	if hwnd == 0 {
		return fmt.Errorf("empty panel HWND")
	}
	original, _, _ := procSetWindowLongPtrW.Call(hwnd, ^uintptr(3), panelWindowProcCallback)
	if original == 0 {
		return fmt.Errorf("SetWindowLongPtrW(GWLP_WNDPROC) failed")
	}
	panelWindowHooks.Lock()
	panelWindowHooks.windows[hwnd] = panelWindowHook{original: original, actions: actions}
	panelWindowHooks.Unlock()
	return nil
}

func panelWindowProc(hwnd, msg, wp, lp uintptr) uintptr {
	panelWindowHooks.RLock()
	hook := panelWindowHooks.windows[hwnd]
	panelWindowHooks.RUnlock()
	if msg == 0x0010 && hook.actions != nil && hook.actions.trayReady.Load() && !hook.actions.quitRequested.Load() {
		procShowWindow.Call(hwnd, 0)
		return 0
	}
	r, _, _ := procCallWindowProcW.Call(hook.original, hwnd, msg, wp, lp)
	if msg == 0x0082 {
		panelWindowHooks.Lock()
		delete(panelWindowHooks.windows, hwnd)
		panelWindowHooks.Unlock()
	}
	return r
}

func daemonExecutableOrEmpty(dataDir string) string {
	exe, err := daemonExecutable()
	if err != nil {
		return ""
	}
	return exe
}

func launcherLog(dataDir, line string) {
	f, err := os.OpenFile(filepath.Join(dataDir, "launcher.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), line)
}
