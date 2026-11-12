//go:build windows

package tray

import (
	"fmt"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
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

// The tray follows the Windows display language when [ui] language is "system".
var procGetUserDefaultUILanguage = modKernel32.NewProc("GetUserDefaultUILanguage")

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

// Actions is what the tray asks its owner (the serve daemon) to do. Both are
// called on the tray thread, so they must return promptly.
type Actions interface {
	ShowPanel()

	Quit()
}

// Languager is optionally implemented by Actions to give the [ui] language
// setting ("system", "en" or "ja"). It is read each time the menu opens, so a
// change in the panel applies without restarting.
type Languager interface {
	Language() string
}

// menuItem is one row of the tray's context menu. New entries only need a
// row here: the popup is built from this table and WM_COMMAND is routed
// through it. Labels holds each language's text; English is the fallback.
type menuItem struct {
	ID     uintptr
	Labels map[string]string
	run    func(*Panel)
}

var menuItems = []menuItem{
	{cmdOpenPanel, map[string]string{"en": "Open control panel", "ja": "コントロールパネルを開く"}, func(p *Panel) { p.actions.ShowPanel() }},
	{cmdQuit, map[string]string{"en": "Quit Yozora", "ja": "Yozora を終了"}, func(p *Panel) {
		p.actions.Quit()
		p.PostClose()
	}},
}

// langJapanese is LANG_JAPANESE, the primary language of a Windows LANGID.
const langJapanese = 0x11

// menuLanguage resolves a [ui] language setting. "system" follows the Windows
// display language (uiLang is a LANGID); anything unknown falls back to English.
func menuLanguage(setting string, uiLang uint16) string {
	switch setting {
	case "en", "ja":
		return setting
	case "", "system":
		if uiLang&0x3ff == langJapanese {
			return "ja"
		}
	}
	return "en"
}

// menuLabels returns the menu's labels, in menu order, for a resolved language.
func menuLabels(lang string) []string {
	out := make([]string, len(menuItems))
	for i, item := range menuItems {
		if out[i] = item.Labels[lang]; out[i] == "" {
			out[i] = item.Labels["en"]
		}
	}
	return out
}

func (p *Panel) language() string {
	setting := "system"
	if l, ok := p.actions.(Languager); ok {
		setting = l.Language()
	}
	id, _, _ := procGetUserDefaultUILanguage.Call()
	return menuLanguage(setting, uint16(id))
}

type Panel struct {
	tooltip string
	actions Actions

	// hostHwnd is written on the tray thread and read by PostClose from any
	// goroutine.
	hostHwnd atomic.Uintptr
	icon     uintptr
	// iconAdded is touched only on the tray thread.
	iconAdded bool
	closing   atomic.Bool

	taskbarCreated uintptr
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
		return p.runCommand(lp)
	}
	return false
}

func (p *Panel) runCommand(id uintptr) bool {
	for _, item := range menuItems {
		if item.ID == id {
			item.run(p)
			return true
		}
	}
	return false
}

func (p *Panel) popupMenu() {
	menu, _, _ := procCreatePopupMenuW.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	labels := menuLabels(p.language())
	for i, item := range menuItems {
		appendMenuItem(menu, item.ID, labels[i])
	}

	host := p.HostHandle()
	procSetForeground.Call(host)
	var pt struct{ X, Y int32 }
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	chosen, _, _ := procTrackPopupMenuEx.Call(menu,
		tpmRightAlign|tpmBottomAlign|tpmRightButton|tpmReturnCmd,
		uintptr(pt.X), uintptr(pt.Y), host, 0)
	if chosen != 0 {
		p.dispatchMenuSelection(chosen)
	}
	procPostMessageW.Call(host, wmNull, 0, 0)
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

// Run creates the hidden host window and icon and pumps their messages until
// PostClose or the Quit menu item. It blocks, so callers run it on its own
// goroutine; the OS thread stays locked for the window's life.
func (p *Panel) Run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	name, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	p.taskbarCreated, _, _ = procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(name)))
	hwnd := createHostWindow(p)
	if hwnd == 0 {
		return fmt.Errorf("tray: create host window failed")
	}
	p.hostHwnd.Store(hwnd)
	icon := loadTrayIcon()
	if icon == 0 {
		p.destroyHost()
		return fmt.Errorf("tray: no usable icon")
	}
	p.icon = icon
	defer func() {
		p.removeIcon()
		procDestroyIcon.Call(icon)
		p.destroyHost()
	}()
	// A close requested before the window existed found no handle to post to.
	if p.closing.Load() {
		return nil
	}
	if !p.addIcon() {
		// At sign-in the daemon can start before Explorer's taskbar. The
		// TaskbarCreated broadcast adds the icon once the taskbar is up.
		log.Println("tray: taskbar not ready; the icon will appear when it starts")
	}
	p.messageLoop()
	return nil
}

func (p *Panel) destroyHost() {
	if h := p.hostHwnd.Swap(0); h != 0 {
		procDestroyWindow.Call(h)
	}
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

func (p *Panel) HostHandle() uintptr { return p.hostHwnd.Load() }

// PostClose asks the tray thread to remove the icon and end Run. It is safe
// from any goroutine, repeatable, and effective even before Run has created
// its window.
func (p *Panel) PostClose() {
	p.closing.Store(true)
	if h := p.HostHandle(); h != 0 {
		procPostMessageW.Call(h, wmClose, 0, 0)
	}
}

func (p *Panel) addIcon() bool {
	h := p.HostHandle()
	if h == 0 || p.icon == 0 {
		return false
	}
	p.iconAdded = addTrayIcon(h, p.icon)
	return p.iconAdded
}

func (p *Panel) removeIcon() {
	h := p.HostHandle()
	if h == 0 || !p.iconAdded {
		return
	}
	p.iconAdded = false
	n := notifyData(h, 0)
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
	if p != nil && p.taskbarCreated != 0 && msg == p.taskbarCreated {
		if !p.closing.Load() {
			p.addIcon()
		}
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
		if p != nil {
			p.hostHwnd.CompareAndSwap(hwnd, 0)
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
