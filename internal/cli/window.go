//go:build windows

package cli

import (
	"context"
	"errors"
	"fmt"
	"log"
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

	"uika-resonance/internal/config"
	"uika-resonance/internal/presence"
	"uika-resonance/internal/tray"
	"uika-resonance/internal/update"
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
		setPanelEntry(entry, panelHwnd)
	}
	return entry
}

// focusRunningPanel brings an open control panel to the front. It reports
// false when no panel window is live, including while one is closing.
func focusRunningPanel() bool {
	title, _ := syscall.UTF16PtrFromString(panelEntryTitle)
	class, _ := syscall.UTF16PtrFromString(panelEntryClass)
	entry, _, _ := procFindWindowExW.Call(hwndMessage, 0,
		uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)))
	if entry == 0 {
		return false
	}
	prop, _ := syscall.UTF16PtrFromString(panelHWNDProp)
	panel, _, _ := procGetPropW.Call(entry, uintptr(unsafe.Pointer(prop)))
	if panel == 0 {
		return false
	}
	if live, _, _ := procIsPanelWindow.Call(panel); live == 0 {
		return false
	}
	procShowWindow.Call(panel, 9) // SW_RESTORE
	procForeground.Call(panel)
	return true
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
	session := &panelSession{dataDir: dataDir}
	session.initEntry()
	defer procDestroyPanelWindow.Call(session.entry)
	// The panel lives only as long as the daemon that serves it: Quit Yozora,
	// a crash, or a shutdown that races this launch all close it.
	go watchDaemonExit(endpoint.PID, dataDir, session.Quit)
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
	return runPanelLifecycle(session, u.String(), factory)
}

// panelSession is the state of the one panel window this process shows.
type panelSession struct {
	mu            sync.Mutex
	panel         panelView
	entry         uintptr
	panelHwnd     uintptr
	dataDir       string
	quitRequested atomic.Bool
}

func (a *panelSession) initEntry() {
	a.entry = createPanelEntry(0)
	if a.entry == 0 {
		launcherLog(a.dataDir, "panel entry unavailable; a second launch cannot focus this panel")
	}
}

// Quit closes the panel from any goroutine. A quit that lands while the window
// is still being created is honoured once runPanelLifecycle publishes it.
func (a *panelSession) Quit() {
	a.quitRequested.Store(true)
	a.closePanel()
}

func (a *panelSession) closePanel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.panelHwnd != 0 {
		procPostPanelMessage.Call(a.panelHwnd, 0x0010, 0, 0)
	}
}

const (
	synchronize     = 0x00100000
	waitInfinite    = 0xFFFFFFFF
	errInvalidParam = syscall.Errno(87)
)

var (
	procOpenProcess         = syscall.NewLazyDLL("kernel32.dll").NewProc("OpenProcess")
	procWaitForSingleObject = syscall.NewLazyDLL("kernel32.dll").NewProc("WaitForSingleObject")
	procCloseHandle         = syscall.NewLazyDLL("kernel32.dll").NewProc("CloseHandle")
)

// watchDaemonExit calls onExit once the daemon process pid has ended. It parks
// in one blocking wait, so it costs no CPU while the panel is open.
func watchDaemonExit(pid int, dataDir string, onExit func()) {
	h, _, err := procOpenProcess.Call(synchronize, 0, uintptr(pid))
	if h == 0 {
		if err == errInvalidParam {
			onExit() // Already gone.
			return
		}
		launcherLog(dataDir, fmt.Sprintf("cannot watch daemon pid %d: %v", pid, err))
		return
	}
	defer procCloseHandle.Call(h)
	procWaitForSingleObject.Call(h, waitInfinite)
	onExit()
}

type panelWindowHook struct {
	original uintptr
	actions  *panelSession
}

var panelWindowHooks = struct {
	sync.RWMutex
	windows map[uintptr]panelWindowHook
}{windows: make(map[uintptr]panelWindowHook)}

var panelWindowProcCallback = syscall.NewCallback(panelWindowProc)

func installPanelWindowHook(hwnd uintptr, actions *panelSession) error {
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
	if (msg == 0x0010 || msg == 0x0002) && hook.actions != nil {
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
	procPostPanelMessage   = syscall.NewLazyDLL("user32.dll").NewProc("PostMessageW")
	procIsPanelWindow      = syscall.NewLazyDLL("user32.dll").NewProc("IsWindow")
	procDestroyPanelWindow = syscall.NewLazyDLL("user32.dll").NewProc("DestroyWindow")
)

func setPanelEntry(entry, panel uintptr) {
	if entry == 0 {
		return
	}
	prop, _ := syscall.UTF16PtrFromString(panelHWNDProp)
	procSetPropW.Call(entry, uintptr(unsafe.Pointer(prop)), panel)
}

// runPanelLifecycle shows one panel window and returns when it closes; the
// process then exits. It reports false only when no view could be created, so
// the caller can fall back to the browser.
func runPanelLifecycle(a *panelSession, url string, factory func() panelView) bool {
	if a.quitRequested.Load() {
		return true
	}
	view := factory()
	if view == nil {
		launcherLog(a.dataDir, "webview unavailable; falling back to browser")
		return false
	}
	hwnd := uintptr(view.Window())
	a.mu.Lock()
	a.panel, a.panelHwnd = view, hwnd
	a.mu.Unlock()
	setPanelEntry(a.entry, hwnd)
	if err := installPanelWindowHook(hwnd, a); err != nil {
		launcherLog(a.dataDir, "panel close hook: "+err.Error())
	}
	view.Navigate(url)
	// Quit stores the flag before reading panelHwnd under the lock, so a quit
	// that ran while the factory was busy is caught here.
	if a.quitRequested.Load() {
		a.closePanel()
	}
	view.Run()
	a.mu.Lock()
	remaining := a.panel
	a.panel, a.panelHwnd = nil, 0
	a.mu.Unlock()
	if remaining != nil {
		remaining.teardown()
	}
	setPanelEntry(a.entry, 0)
	return true
}

var procAllowSetForegroundWindow = syscall.NewLazyDLL("user32.dll").NewProc("AllowSetForegroundWindow")

const asfwAny = uintptr(0xFFFFFFFF)

// daemonTray is the serve daemon's side of the tray icon. The icon lives as
// long as the daemon; the panel is a separate, short-lived process.
type daemonTray struct {
	cfg       *config.Holder
	stop      func() // cancels serve's context
	dataDir   string
	panelLive bool // the dashboard server is listening and its endpoint written
	eng       *presence.Engine
	quitting  atomic.Bool
	update    *update.Service
}

// UpdateVersion is the newer version the tray offers, or "" when there is none.
func (d *daemonTray) UpdateVersion() string {
	if d.update == nil || !d.update.Active() || !d.cfg.Get().Update.Check {
		return ""
	}
	v, _ := d.update.Available()
	return v
}

// InstallUpdate runs when the user picks the tray's update row. Progress and
// errors are only visible in the panel, so a failure is also said out loud.
func (d *daemonTray) InstallUpdate() {
	go func() {
		if err := d.update.Install(context.Background()); err != nil && !errors.Is(err, update.ErrBusy) {
			log.Printf("tray: update: %v", err)
			launcherNotify("Yozora", "Could not update Yozora:\n"+err.Error(), notifyError)
		}
	}()
}

func (d *daemonTray) ShowPanel() {
	if d.quitting.Load() {
		return
	}
	if !d.panelLive {
		go d.explainPanelOff()
		return
	}
	if focusRunningPanel() {
		return
	}
	// Explorer lets the icon's owner take the foreground after a click; pass
	// that on so the panel process can raise its window.
	procAllowSetForegroundWindow.Call(asfwAny)
	exe, err := daemonExecutable()
	if err == nil {
		err = spawnPanel(exe)
	}
	if err != nil {
		log.Printf("tray: open control panel: %v", err)
	}
}

func (d *daemonTray) explainPanelOff() {
	cfgPath, _, _ := config.Paths()
	if err := ensureDashboardConfig(cfgPath); err != nil {
		launcherNotify("Yozora", "Could not prepare the settings file:\n"+err.Error(), notifyError)
		return
	}
	launcherNotify("Yozora", "The control panel is not available in this session. Quit Yozora and open it again to use it.", notifyInfo)
}

// Language gives the tray menu the panel's language setting.
func (d *daemonTray) Language() string {
	if d.cfg == nil {
		return config.DefaultLanguage
	}
	return d.cfg.Get().UI.Language
}

func (d *daemonTray) Quit() {
	if !d.quitting.CompareAndSwap(false, true) {
		return
	}
	log.Println("tray: quit requested")
	d.stop()
}

// startDaemonTray shows the tray icon for the life of the serve daemon. The
// returned func removes the icon and waits briefly for the tray thread, so no
// stale icon is left behind when the process exits.
func startDaemonTray(cfg *config.Holder, stop func(), dataDir string, panelLive bool, eng *presence.Engine, upd *update.Service) func() {
	t := tray.New(&daemonTray{cfg: cfg, stop: stop, dataDir: dataDir, panelLive: panelLive, eng: eng, update: upd})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := t.Run(); err != nil {
			log.Printf("tray: %v", err)
		}
	}()
	return func() {
		t.PostClose()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}
