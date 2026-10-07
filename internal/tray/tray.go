//go:build windows

package tray

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modShell    = syscall.NewLazyDLL("shell32.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW  = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW   = modUser32.NewProc("DefWindowProcW")
	procDestroyWindow    = modUser32.NewProc("DestroyWindow")
	procPostQuitMessage  = modUser32.NewProc("PostQuitMessage")
	procLoadImageW       = modUser32.NewProc("LoadImageW")
	procCreatePopupMenuW = modUser32.NewProc("CreatePopupMenu")
	procAppendMenuW      = modUser32.NewProc("AppendMenuW")
	procTrackPopupMenuEx = modUser32.NewProc("TrackPopupMenuEx")
	procDestroyMenu      = modUser32.NewProc("DestroyMenu")
	procSetForeground    = modUser32.NewProc("SetForegroundWindow")
	procGetCursorPos     = modUser32.NewProc("GetCursorPos")
	procGetModuleHandleW = modKernel32.NewProc("GetModuleHandleW")
	procShellNotifyIconW = modShell.NewProc("Shell_NotifyIconW")
	procPostMessageW     = modUser32.NewProc("PostMessageW")
	procGetMessageW      = modUser32.NewProc("GetMessageW")
	procTranslateMessage = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW = modUser32.NewProc("DispatchMessageW")
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

	imageIcon     = 1
	lrDefaultSize = 0x0040
	lrShared      = 0x8000

	iconResource = 1
)

const (
	wmAppTray = 0x8000

	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205

	wmCommand = 0x0111
	wmClose   = 0x0010
	wmDestroy = 0x0002
	wmQuit    = 0x0012
	wmNull    = 0x0000

	swShow = 5

	tpmRightAlign  = 0x0008
	tpmBottomAlign = 0x0020
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	mfString = 0x00000000
)

const (
	cmdOpenPanel = 1001
	cmdQuit      = 1002
)

type Actions interface {
	ShowPanel()

	Quit()
}

type Panel struct {
	tooltip string
	actions Actions

	hostHwnd uintptr
	icon     uintptr

	taskbarCreated uintptr

	dashboardJSON string
	daemonExe     string
}

type recipe struct {
	Tooltip string
	IconID  uintptr
}

func (p *Panel) Recipe() recipe {
	return recipe{Tooltip: p.tip(), IconID: iconResource}
}

func (p *Panel) tip() string {
	if p == nil || p.tooltip == "" {
		return trayTooltip
	}
	return p.tooltip
}

func New(actions Actions) *Panel {
	return &Panel{tooltip: trayTooltip, actions: actions}
}

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
			p.PostClose()
			return true
		}
	}
	return false
}

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
	chosen, _, _ := procTrackPopupMenuEx.Call(menu,
		tpmRightAlign|tpmBottomAlign|tpmRightButton|tpmReturnCmd,
		uintptr(pt.X), uintptr(pt.Y), p.hostHwnd, 0)
	if chosen != 0 {
		p.dispatchMenuSelection(chosen)
	}
	procPostMessageW.Call(p.hostHwnd, wmNull, 0, 0)
}

func (p *Panel) dispatchMenuSelection(command uintptr) bool {
	if command == 0 {
		return false
	}
	return p.Dispatch(wmCommand, command)
}

func appendMenuItem(menu, id uintptr, label string) {
	text, err := syscall.UTF16PtrFromString(label)
	if err != nil {
		return
	}
	procAppendMenuW.Call(menu, mfString, id, uintptr(unsafe.Pointer(text)))
}

func (p *Panel) Run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	name, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	p.taskbarCreated, _, _ = procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(name)))
	p.hostHwnd = createHostWindow(p)
	if p.hostHwnd == 0 {
		return fmt.Errorf("tray: create host window failed")
	}
	icon := loadTrayIcon()
	if icon == 0 {
		procDestroyWindow.Call(p.hostHwnd)
		p.hostHwnd = 0
		return fmt.Errorf("tray: no usable icon")
	}
	p.icon = icon
	if !addTrayIcon(p.hostHwnd, icon) {
		procDestroyWindow.Call(p.hostHwnd)
		p.hostHwnd = 0
		return fmt.Errorf("tray: Shell_NotifyIconW(NIM_ADD) failed")
	}
	defer func() {
		p.removeIcon()
		procDestroyIcon.Call(icon)
		if p.hostHwnd != 0 {
			procDestroyWindow.Call(p.hostHwnd)
			p.hostHwnd = 0
		}
	}()
	if ready, ok := p.actions.(interface{ TrayReady() }); ok {
		ready.TrayReady()
	}
	p.messageLoop()
	return nil
}

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
		if r == 0 || r == ^uintptr(0) {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (p *Panel) HostHandle() uintptr { return p.hostHwnd }

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

func addTrayIcon(wnd uintptr, icon uintptr) bool {
	n := notifyData(wnd, icon)
	r, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&n)))
	return r != 0
}

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

func createHostWindow(p *Panel) uintptr {
	class, err := syscall.UTF16PtrFromString(trayClassName)
	if err != nil {
		return 0
	}
	var wc wndClassExW
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	wc.wndProc = syscall.NewCallback(trayWndProc)
	wc.className = class

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	title, _ := syscall.UTF16PtrFromString("Yozora tray host")
	hwnd, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, 0, 0, moduleHandle(), 0)
	if hwnd == 0 {
		return 0
	}

	panels.Store(hwnd, p)
	return hwnd
}

func trayWndProc(hwnd, msg, wp, lp uintptr) uintptr {
	p := panelOf(hwnd)
	if p != nil && p.taskbarCreated != 0 && msg == p.taskbarCreated && p.icon != 0 {
		addTrayIcon(hwnd, p.icon)
		return 0
	}
	if p != nil {
		value := lp
		if msg == wmCommand {
			value = wp & 0xffff
		}
		if p.Dispatch(msg, value) {
			return 0
		}
	}
	switch msg {
	case wmClose:
		if p != nil {
			p.removeIcon()
		}
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		panels.Delete(hwnd)
		procPostQuitMessage.Call(0)
		if p != nil && p.hostHwnd == hwnd {
			p.hostHwnd = 0
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

// panels maps a host window handle to its panel. Storing the *Panel in
// GWLP_USERDATA would need a uintptr -> unsafe.Pointer round-trip that the GC
// cannot see and checkptr rejects; a Go-side map keeps the same lookup safely.
var panels sync.Map // map[uintptr]*Panel

func panelOf(hwnd uintptr) *Panel {
	if p, ok := panels.Load(hwnd); ok {
		return p.(*Panel)
	}
	return nil
}

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
