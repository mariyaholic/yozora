// Package tray shows the Yozora control panel's icon in the system tray
// (notification area) for as long as the launcher process is alive.
//
// The Windows-specific pieces — the hidden host window whose WndProc receives
// the Shell_NotifyIcon callback, and the Shell_NotifyIconW calls themselves —
// are thin wrappers. The behavior on tray events (what a click does, which
// menu entries exist, what quitting means) lives in a small dispatch layer
// that the tests drive directly with synthetic messages, so no test ever
// touches a real tray icon.
//
//go:build windows

package tray

import (
	"syscall"
	"unsafe"
)

var (
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modShell    = syscall.NewLazyDLL("shell32.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW  = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW   = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW    = modUser32.NewProc("DefWindowProcW")
	procDestroyWindow     = modUser32.NewProc("DestroyWindow")
	procPostQuitMessage   = modUser32.NewProc("PostQuitMessage")
	procLoadImageW        = modUser32.NewProc("LoadImageW")
	procCreatePopupMenuW  = modUser32.NewProc("CreatePopupMenu")
	procAppendMenuW       = modUser32.NewProc("AppendMenuW")
	procTrackPopupMenuEx  = modUser32.NewProc("TrackPopupMenuEx")
	procDestroyMenu       = modUser32.NewProc("DestroyMenu")
	procSetForeground     = modUser32.NewProc("SetForegroundWindow")
	procGetCursorPos      = modUser32.NewProc("GetCursorPos")
	procGetModuleHandleW  = modKernel32.NewProc("GetModuleHandleW")
	procShellNotifyIconW  = modShell.NewProc("Shell_NotifyIconW")
	procPostMessageW      = modUser32.NewProc("PostMessageW")
	procGetMessageW       = modUser32.NewProc("GetMessageW")
	procSetWindowLongPtrW = modUser32.NewProc("SetWindowLongPtrW")
	procGetWindowLongPtrW = modUser32.NewProc("GetWindowLongPtrW")
)

const (
	trayClassName = "YozoraTrayHost"
	trayTooltip   = "Yozora"

	nimAdd    = 0
	nimModify = 1
	nimDelete = 2

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	imageIcon = 2      // IMAGE_ICON
	lrShared  = 0x8000 // LR_SHARED

	// iconResource is the resource id under which both binaries'
	// .syso embeds assets/yozora.ico.
	iconResource = 1
)

const (
	// wmAppTray is the message Shell_NotifyIconW posts to the host window.
	wmAppTray = 0x8000 // WM_APP

	// Mouse events delivered in lParam of wmAppTray.
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205

	// Popup-menu plumbing on the host window.
	wmCommand = 0x0111
	wmClose   = 0x0010
	wmDestroy = 0x0002
	wmQuit    = 0x0012

	swShow = 5

	tpmRightAlign  = 0x0008
	tpmBottomAlign = 0x0020
	tpmNonotify    = 0x0080
	tpmRightButton = 0x0002

	mfString = 0x00000000
)

// Menu item identifiers for the right-click popup.
const (
	cmdOpenPanel = 1001
	cmdQuit      = 1002
)

// Actions is what the tray does on user interaction. The indirection keeps
// the message-driven behavior independent of the Windows plumbing: tests
// supply their own implementation and send synthetic events; production
// binds the launcher's WebView2 panel.
type Actions interface {
	// ShowPanel opens or focuses the control-panel window.
	ShowPanel()
	// Quit closes the panel, stops the daemon and ends the launcher.
	Quit()
}

// Panel owns one tray icon for the launcher process. It exists from the
// moment the control panel opens until the user quits Yozora from the tray
// menu (or the launcher process dies).
type Panel struct {
	tooltip string
	actions Actions

	hostHwnd uintptr
	icon     uintptr

	// dashboardJSON and daemonExe feed the guarded daemon stop on Quit;
	// New fills both for production, tests may override.
	dashboardJSON string
	daemonExe     string
}

// recipe mirrors the NOTIFYICONDATA fields this package fills in; it exists
// so tests can assert the notify-icon shape without calling the API.
type recipe struct {
	Tooltip string
	IconID  uintptr
}

// Recipe describes the icon the panel registers.
func (p *Panel) Recipe() recipe {
	return recipe{Tooltip: p.tip(), IconID: iconResource}
}

// tip is the tooltip text carried on the notify icon.
func (p *Panel) tip() string {
	if p == nil || p.tooltip == "" {
		return trayTooltip
	}
	return p.tooltip
}

// New returns a Panel bound to actions with no OS calls performed.
func New(actions Actions) *Panel {
	return &Panel{tooltip: trayTooltip, actions: actions}
}

// Dispatch routes a tray notification or menu selection to the matching
// action. msg/lp are the WndProc message pair (WM_APP+n with the mouse event
// in lp, or WM_COMMAND with the menu id in lp). It is the seam the tests
// exercise with synthetic events. Reports whether the message was handled.
func (p *Panel) Dispatch(msg, lp uintptr) bool {
	switch msg {
	case wmAppTray:
		switch lp {
		case wmLButtonUp, wmLButtonDblClk:
			p.actions.ShowPanel()
			return true
		case wmRButtonUp:
			p.popupMenu()
			return true
		}
	case wmCommand:
		switch lp {
		case cmdOpenPanel:
			p.actions.ShowPanel()
			return true
		case cmdQuit:
			p.actions.Quit()
			return true
		}
	}
	return false
}

// menuLabels returns the popup entries in display order; appendMenuItem is
// invoked with exactly these in production.
func menuLabels() []struct {
	ID    uintptr
	Label string
} {
	return []struct {
		ID    uintptr
		Label string
	}{
		{cmdOpenPanel, "Open control panel"},
		{cmdQuit, "Quit Yozora"},
	}
}

func (p *Panel) popupMenu() {
	menu, _, _ := procCreatePopupMenuW.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	for _, item := range menuLabels() {
		appendMenuItem(menu, item.ID, item.Label)
	}

	procSetForeground.Call(p.hostHwnd)
	var pt struct{ X, Y int32 }
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procTrackPopupMenuEx.Call(menu,
		tpmRightAlign|tpmBottomAlign|tpmNonotify|tpmRightButton,
		uintptr(pt.X), uintptr(pt.Y), p.hostHwnd, 0)
}

func appendMenuItem(menu, id uintptr, label string) {
	text, err := syscall.UTF16PtrFromString(label)
	if err != nil {
		return
	}
	procAppendMenuW.Call(menu, mfString, id, uintptr(unsafe.Pointer(text)))
}

// Run creates the host window, registers the tray icon and pumps messages
// until WM_QUIT. Call it on a thread that stays alive — in the launcher that
// is a background goroutine started alongside the panel window.
func (p *Panel) Run() {
	p.hostHwnd = createHostWindow(p)
	if p.hostHwnd == 0 {
		return
	}
	icon, _, _ := procLoadImageW.Call(
		moduleHandle(), iconResource, imageIcon, 0, 0, lrShared)
	p.icon = icon
	addTrayIcon(p.hostHwnd, icon)
	p.messageLoop()
	p.removeIcon()
}

// messageLoop drains the queue until WM_QUIT or GetMessage fails.
func (p *Panel) messageLoop() {
	var m struct {
		hwnd     uintptr
		message  uint32
		wParam   uintptr
		lParam   uintptr
		time     uint32
		pt       struct{ X, Y int32 }
		lPrivate uint32
	}
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 || m.message == wmQuit {
			return
		}
		// Tray and menu messages reach the WndProc via DispatchMessage in a
		// full loop; this minimal loop only needs the quit signal because
		// all input lands on the host window's own procedure.
		procDefWindowProcW.Call(m.hwnd, uintptr(m.message), m.wParam, m.lParam)
	}
}

// HostHandle returns the tray host window's HWND (0 before Run).
func (p *Panel) HostHandle() uintptr { return p.hostHwnd }

// PostClose asks the tray loop to end (WM_CLOSE to the hidden host). Safe to
// call before Run or after it exits.
func (p *Panel) PostClose() {
	if h := p.HostHandle(); h != 0 {
		procPostMessageW.Call(h, wmClose, 0, 0)
	}
}

func (p *Panel) removeIcon() {
	if p.hostHwnd == 0 {
		return
	}
	n := notifyData(p.hostHwnd, 0)
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&n)))
}

// notifyData assembles the NOTIFYICONDATAW this package fills in.
func notifyData(wnd uintptr, icon uintptr) notifyIconDataW {
	var n notifyIconDataW
	n.cbSize = uint32(unsafe.Sizeof(n))
	n.hWnd = wnd
	n.uID = 1
	n.uFlags = nifMessage | nifIcon | nifTip
	n.uCallbackMessage = wmAppTray
	n.hIcon = icon
	if t, err := syscall.UTF16FromString(trayTooltip); err == nil {
		copy(n.szTip[:], t)
	}
	return n
}

func addTrayIcon(wnd uintptr, icon uintptr) {
	n := notifyData(wnd, icon)
	procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&n)))
}

// notifyIconDataW is NOTIFYICONDATAW (V2 layout, flags scoped to what the
// tray uses). Field order and padding must match the Win32 declaration.
type notifyIconDataW struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     uintptr
}

// createHostWindow registers the tray host class (registration is idempotent
// within a process) and creates a hidden top-level window whose WndProc is
// trayWndProc; the *Panel rides along as window userdata.
func createHostWindow(p *Panel) uintptr {
	class, err := syscall.UTF16PtrFromString(trayClassName)
	if err != nil {
		return 0
	}
	var wc wndClassExW
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	wc.wndProc = syscall.NewCallback(trayWndProc)
	wc.className = class
	// Re-registering an existing class fails harmlessly.
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	title, _ := syscall.UTF16PtrFromString("Yozora tray host")
	hwnd, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, 0, 0, moduleHandle(), 0)
	if hwnd == 0 {
		return 0
	}
	// Attach the panel pointer for WndProc lookup.
	procSetWindowLongPtrW.Call(hwnd, uintptr(gwlpUserDataAsInt()), uintptr(unsafe.Pointer(p)))
	return hwnd
}

// trayWndProc is the hidden host window's procedure. It maps tray clicks and
// menu picks onto the Panel actions and lets everything else through.
func trayWndProc(hwnd, msg, wp, lp uintptr) uintptr {
	p := panelOf(hwnd)
	if p != nil && p.Dispatch(msg, lp) {
		return 0
	}
	switch msg {
	case wmClose, wmDestroy:
		procPostQuitMessage.Call(0)
		if p != nil && p.hostHwnd == hwnd {
			p.hostHwnd = 0
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

// windowProps reads the *Panel attached as GWLP_USERDATA on the host window.
func panelOf(hwnd uintptr) *Panel {
	val, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(gwlpUserDataAsInt()))
	if val == 0 {
		return nil
	}
	return (*Panel)(unsafe.Pointer(val))
}

const gwlpUserData = -21

// gwlpUserDataAsInt returns GWLP_USERDATA as its Win32 type: a signed index
// into the window's metadata, negative by design.
func gwlpUserDataAsInt() int32 { return gwlpUserData }

// wndClassExW is the WNDCLASSEXW layout for RegisterClassExW.
type wndClassExW struct {
	cbSize     uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   uintptr
	className  *uint16
	iconSmall  uintptr
}

func moduleHandle() uintptr {
	h, _, _ := procGetModuleHandleW.Call(0)
	return h
}
