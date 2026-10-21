//go:build windows

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	webview "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/pkg/edge"

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

func createPanelEntry(panelHwnd uintptr) uintptr {
	title, _ := syscall.UTF16PtrFromString(panelEntryTitle)
	class, _ := syscall.UTF16PtrFromString(panelEntryClass)
	entry, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, hwndMessage, 0, 0, 0)
	if entry != 0 {
		prop, _ := syscall.UTF16PtrFromString(panelHWNDProp)
		procSetPropW.Call(entry, uintptr(unsafe.Pointer(prop)), panelHwnd)
	}
	return entry
}

func activateRunningPanel() int {
	title, _ := syscall.UTF16PtrFromString(panelEntryTitle)
	class, _ := syscall.UTF16PtrFromString(panelEntryClass)
	entry, _, _ := procFindWindowExW.Call(hwndMessage, 0,
		uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)))
	if entry == 0 {

		return 0
	}
	showPanelEntry(entry)
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
	actions := &panelTrayActions{
		dashboardJSON: dashboardEndpointPath(dataDir),
		daemonExe:     daemonExecutableOrEmpty(dataDir), dataDir: dataDir,
	}
	actions.initEntry()
	defer procDestroyPanelWindow.Call(actions.entry)
	factory := func() panelView {
		w := webview.NewWithOptions(webview.WebViewOptions{
			AutoFocus: true, DataPath: filepath.Join(dataDir, "webview"),
			WindowOptions: webview.WindowOptions{Title: "Yozora", Width: width, Height: height, IconId: 1, Center: true},
		})
		if w == nil {
			return nil
		}
		return &nativePanelView{WebView: w, dataDir: dataDir}
	}

	trayPanel := tray.New(actions)
	trayDone := make(chan struct{})
	go func() {
		defer close(trayDone)
		if err := trayPanel.Run(); err != nil {
			launcherLog(dataDir, err.Error())
		}
		actions.trayStopped()
	}()
	defer func() {

		trayPanel.PostClose()
		select {
		case <-trayDone:
		case <-time.After(2 * time.Second):
		}
	}()

	return runPanelLifecycle(actions, u.String(), factory)
}

type panelTrayActions struct {
	mu              sync.Mutex
	panel           panelView
	entry           uintptr
	panelHwnd       uintptr
	dashboardJSON   string
	daemonExe       string
	dataDir         string
	trayReady       atomic.Bool
	reopenRequested atomic.Bool
	quitRequested   atomic.Bool
}

func (a *panelTrayActions) initEntry() {
	a.entry = createPanelEntry(0)
	if a.entry != 0 {
		if err := installPanelWindowHook(a.entry, a); err == nil {
			return
		} else {
			launcherLog(a.dataDir, "panel entry hook: "+err.Error())
		}
		procDestroyPanelWindow.Call(a.entry)
		a.entry = 0
	}
	launcherLog(a.dataDir, "panel entry unavailable; closing the panel will exit")
}

func (a *panelTrayActions) trayStopped() {
	a.trayReady.Store(false)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.panelHwnd == 0 && a.entry != 0 {
		procPostPanelMessage.Call(a.entry, 0, 0, 0)
	}
}

func (a *panelTrayActions) ShowPanel() {
	showPanelEntry(a.entry)
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
	a.closePanel()
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
	setter := procSetWindowLongPtrW
	if unsafe.Sizeof(uintptr(0)) == 4 {
		setter = syscall.NewLazyDLL("user32.dll").NewProc("SetWindowLongW")
	}
	original, _, _ := setter.Call(hwnd, ^uintptr(3), panelWindowProcCallback)
	if original == 0 {
		return fmt.Errorf("%s(GWLP_WNDPROC) failed", setter.Name)
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
	if hook.actions != nil && hwnd == hook.actions.entry && msg == panelReopenMessage {
		a := hook.actions
		a.mu.Lock()
		closed := a.panelHwnd == 0
		a.mu.Unlock()
		if closed {
			a.reopenRequested.Store(true)
			procPostPanelMessage.Call(hwnd, 0, 0, 0)
		}
		return 0
	}
	if (msg == 0x0010 || msg == 0x0002) && hook.actions != nil && hwnd != hook.actions.entry {
		a := hook.actions
		a.mu.Lock()
		view := a.panel
		a.panel = nil
		a.panelHwnd = 0
		a.mu.Unlock()
		setPanelEntry(a.entry, 0)
		if view != nil {
			view.teardown()
		}
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

// panelView keeps lifecycle tests independent of an installed WebView2 runtime.
type panelView interface {
	Navigate(string)
	Run()
	Window() unsafe.Pointer
	teardown()
}

type nativePanelView struct {
	webview.WebView
	dataDir string
	closed  bool // Accessed only by the locked panel thread.
}

func chromiumFromView(view any) (*edge.Chromium, bool) {
	v := reflect.ValueOf(view)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return nil, false
	}
	field := v.Elem().FieldByName("browser")
	if !field.IsValid() || !field.CanAddr() {
		return nil, false
	}
	value := reflect.NewAt(field.Type(), field.Addr().UnsafePointer()).Elem().Interface()
	chromium, ok := value.(*edge.Chromium)
	return chromium, ok && chromium != nil
}

// COM interfaces begin with a vtable pointer. Retain pointers as pointers until
// the syscall boundary so vet/checkptr can track their provenance.
func callPanelCOM(object unsafe.Pointer, slot int) uintptr {
	table := *(*unsafe.Pointer)(object)
	method := *(*uintptr)(unsafe.Add(table, uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(method, uintptr(object))
	return r
}

func (v *nativePanelView) teardown() {
	if v.closed {
		return
	}
	v.closed = true
	browser, ok := chromiumFromView(v.WebView)
	if !ok {
		launcherLog(v.dataDir, "webview teardown: browser field unavailable or incompatible")
		return
	}
	if controller := browser.GetController(); controller != nil {
		hr := callPanelCOM(unsafe.Pointer(controller), 24) // ICoreWebView2Controller::Close
		if int32(hr) < 0 {
			launcherLog(v.dataDir, fmt.Sprintf("webview Close: HRESULT %#x", hr))
		}
		callPanelCOM(unsafe.Pointer(controller), 2) // Balance the library's AddRef.
	}
	if environment := browser.Environment(); environment != nil {
		callPanelCOM(unsafe.Pointer(environment), 2)
	}
	runtime.KeepAlive(browser)
}

var (
	procRegisterPanelMessage  = syscall.NewLazyDLL("user32.dll").NewProc("RegisterWindowMessageW")
	procPostPanelMessage      = syscall.NewLazyDLL("user32.dll").NewProc("PostMessageW")
	procIsPanelWindow         = syscall.NewLazyDLL("user32.dll").NewProc("IsWindow")
	procDestroyPanelWindow    = syscall.NewLazyDLL("user32.dll").NewProc("DestroyWindow")
	procPeekPanelMessage      = syscall.NewLazyDLL("user32.dll").NewProc("PeekMessageW")
	procGetPanelMessage       = syscall.NewLazyDLL("user32.dll").NewProc("GetMessageW")
	procTranslatePanelMessage = syscall.NewLazyDLL("user32.dll").NewProc("TranslateMessage")
	procDispatchPanelMessage  = syscall.NewLazyDLL("user32.dll").NewProc("DispatchMessageW")
	panelReopenMessage        = registerPanelMessage()
)

func registerPanelMessage() uintptr {
	name, _ := syscall.UTF16PtrFromString("YozoraReopenPanel")
	msg, _, _ := procRegisterPanelMessage.Call(uintptr(unsafe.Pointer(name)))
	return msg
}

func setPanelEntry(entry, panel uintptr) {
	prop, _ := syscall.UTF16PtrFromString(panelHWNDProp)
	procSetPropW.Call(entry, uintptr(unsafe.Pointer(prop)), panel)
}

func showPanelEntry(entry uintptr) {
	if entry == 0 {
		return
	}
	prop, _ := syscall.UTF16PtrFromString(panelHWNDProp)
	panel, _, _ := procGetPropW.Call(entry, uintptr(unsafe.Pointer(prop)))
	live, _, _ := procIsPanelWindow.Call(panel)
	if panel != 0 && live != 0 {
		procShowWindow.Call(panel, 9)
		procForeground.Call(panel)
	} else {
		panelWindowHooks.RLock()
		hook := panelWindowHooks.windows[entry]
		panelWindowHooks.RUnlock()
		if hook.actions != nil {
			hook.actions.reopenRequested.Store(true)
		}
		procPostPanelMessage.Call(entry, panelReopenMessage, 0, 0)
	}
}

func (a *panelTrayActions) closePanel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.panelHwnd != 0 {
		procPostPanelMessage.Call(a.panelHwnd, 0x0010, 0, 0)
	} else if a.entry != 0 {
		// Wake the idle pump; quitRequested/trayReady determine whether to exit.
		procPostPanelMessage.Call(a.entry, 0, 0, 0)
	}
}

type panelMessage struct {
	hwnd           uintptr
	message        uint32
	wParam, lParam uintptr
	time           uint32
	pt             struct{ X, Y int32 }
	lPrivate       uint32
}

func waitPanelReopen(a *panelTrayActions) bool {
	var msg panelMessage
	for {
		if a.quitRequested.Load() || !a.trayReady.Load() || a.entry == 0 {
			return false
		}
		if a.reopenRequested.Load() {
			return true
		}
		r, _, _ := procGetPanelMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if r == 0 || int32(r) == -1 {
			return false
		}
		if a.quitRequested.Load() || !a.trayReady.Load() || a.entry == 0 {
			return false
		}
		if a.reopenRequested.Load() || (msg.hwnd == a.entry && uintptr(msg.message) == panelReopenMessage) {
			return true
		}
		procTranslatePanelMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchPanelMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func runPanelLifecycle(a *panelTrayActions, url string, factory func() panelView) bool {
	for {
		if a.quitRequested.Load() {
			return true
		}
		a.reopenRequested.Store(false)
		view := factory()
		if view == nil {
			launcherLog(a.dataDir, "webview unavailable; falling back to browser")
			return false
		}
		hwnd := uintptr(view.Window())
		a.mu.Lock()
		a.panel, a.panelHwnd = view, hwnd
		a.mu.Unlock()
		// Requests that arrived before or while the factory pumped messages are
		// satisfied by this view.
		if a.entry != 0 {
			var msg panelMessage
			for {
				r, _, _ := procPeekPanelMessage.Call(uintptr(unsafe.Pointer(&msg)), a.entry, panelReopenMessage, panelReopenMessage, 1)
				if r == 0 {
					break
				}
			}
		}
		a.reopenRequested.Store(false)
		setPanelEntry(a.entry, hwnd)
		if err := installPanelWindowHook(hwnd, a); err != nil {
			launcherLog(a.dataDir, "panel close hook: "+err.Error())
		}
		view.Navigate(url)
		// A quit can arrive while the factory is creating the view.
		if a.quitRequested.Load() {
			a.closePanel()
		}
		view.Run()
		a.mu.Lock()
		remaining := a.panel
		a.panel = nil
		a.mu.Unlock()
		if remaining != nil {
			remaining.teardown()
		}
		a.mu.Lock()
		a.panel, a.panelHwnd = nil, 0
		a.mu.Unlock()
		setPanelEntry(a.entry, 0)
		if !waitPanelReopen(a) {
			return true
		}
	}
}
