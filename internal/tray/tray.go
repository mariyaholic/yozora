//go:build windows

package tray

import (
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
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

	mfString    = 0x00000000
	mfGrayed    = 0x00000001
	mfDisabled  = 0x00000002
	mfChecked   = 0x00000008
	mfSeparator = 0x00000800
)

const (
	cmdOpenPanel  = 1001
	cmdQuit       = 1002
	cmdPauseHour  = 1003
	cmdPauseOpen  = 1004
	cmdResume     = 1005
	cmdHeader     = 1010
	cmdSourceBase = 1100 // + index into sourceToggles
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

// Controller is optionally implemented by Actions to feed the richer menu: the
// current track, the pause state and the video-site switches. Without it the
// menu keeps only its fixed rows. Calls come from the tray thread, so they must
// return promptly; anything slow (saving the config) belongs on a goroutine.
type Controller interface {
	// MenuState is read each time the menu opens.
	MenuState() State
	PauseFor(d time.Duration)
	PauseUntilResumed()
	Resume()
	SetSource(key string, on bool)
}

// State is what the menu shows, read when it opens.
type State struct {
	Title, Artist string
	Paused        bool
	// Sources maps a switch key (see sourceToggles) to whether it is on.
	Sources map[string]bool
}

// sourceToggles are the video-site switches, in menu order. The keys are the
// panel's source keys, so both save through the same config path.
var sourceToggles = []struct {
	Key    string
	Labels map[string]string
}{
	{"youtube", map[string]string{"en": "YouTube", "ja": "YouTube"}},
	{"twitch", map[string]string{"en": "Twitch", "ja": "Twitch"}},
	{"youtube_tabs", map[string]string{"en": "Background Edge tabs", "ja": "バックグラウンドの Edge タブ"}},
}

// menuItem is one fixed row of the tray's context menu. New entries only need a
// row here: the popup is built from this table and WM_COMMAND is routed
// through it. Labels holds each language's text; English is the fallback.
type menuItem struct {
	ID     uintptr
	Labels map[string]string
	run    func(*Panel)
}

var menuItems = []menuItem{
	{cmdOpenPanel, map[string]string{"en": "Open control panel", "ja": "コントロールパネルを開く"}, func(p *Panel) { p.actions.ShowPanel() }},
	{cmdPauseHour, map[string]string{"en": "Pause sharing for 1 hour", "ja": "共有を 1 時間一時停止"}, func(p *Panel) {
		if c := p.controller(); c != nil {
			c.PauseFor(time.Hour)
		}
	}},
	{cmdPauseOpen, map[string]string{"en": "Pause until I resume", "ja": "再開するまで一時停止"}, func(p *Panel) {
		if c := p.controller(); c != nil {
			c.PauseUntilResumed()
		}
	}},
	{cmdResume, map[string]string{"en": "Resume sharing", "ja": "共有を再開"}, func(p *Panel) {
		if c := p.controller(); c != nil {
			c.Resume()
		}
	}},
	{cmdQuit, map[string]string{"en": "Quit Yozora", "ja": "Yozora を終了"}, func(p *Panel) {
		p.actions.Quit()
		p.PostClose()
	}},
}

// itemShown decides which state-dependent rows appear. A row not listed here is
// always shown; one listed here needs a Controller, so it never shows without.
var itemShown = map[uintptr]func(State) bool{
	cmdPauseHour: func(s State) bool { return !s.Paused },
	cmdPauseOpen: func(s State) bool { return !s.Paused },
	cmdResume:    func(s State) bool { return s.Paused },
}

// nothingPlaying heads the menu when no track is detected.
var nothingPlaying = map[string]string{"en": "Nothing playing", "ja": "再生中の曲はありません"}

// maxHeaderRunes keeps the track row from stretching the whole menu.
const maxHeaderRunes = 44

func pick(labels map[string]string, lang string) string {
	if l := labels[lang]; l != "" {
		return l
	}
	return labels["en"]
}

// headerLabel is the current track as "Title – Artist", shortened with an ellipsis.
func headerLabel(lang string, st State) string {
	title, artist := strings.TrimSpace(st.Title), strings.TrimSpace(st.Artist)
	if title == "" {
		return pick(nothingPlaying, lang)
	}
	text := title
	if artist != "" {
		text += " \u2013 " + artist
	}
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > maxHeaderRunes {
		text = strings.TrimRight(string(r[:maxHeaderRunes-1]), " ") + "\u2026"
	}
	return text
}

// menuRow is one line of the popup, ready to append.
type menuRow struct {
	ID        uintptr
	Label     string
	Disabled  bool
	Checked   bool
	Separator bool
}

// buildMenu lays the popup out for a language and state: the track header,
// the table's rows that apply, and, ahead of Quit, the video-site switches.
// A nil state (no Controller) gives only the table's unconditional rows.
func buildMenu(lang string, st *State) []menuRow {
	var rows []menuRow
	if st != nil {
		rows = append(rows, menuRow{ID: cmdHeader, Label: headerLabel(lang, *st), Disabled: true})
	}
	for _, item := range menuItems {
		if show, conditional := itemShown[item.ID]; conditional && (st == nil || !show(*st)) {
			continue
		}
		if item.ID == cmdQuit && st != nil {
			rows = append(rows, menuRow{Separator: true})
			for i, toggle := range sourceToggles {
				rows = append(rows, menuRow{ID: cmdSourceBase + uintptr(i), Label: pick(toggle.Labels, lang), Checked: st.Sources[toggle.Key]})
			}
			rows = append(rows, menuRow{Separator: true})
		}
		rows = append(rows, menuRow{ID: item.ID, Label: pick(item.Labels, lang)})
	}
	return rows
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

func (p *Panel) controller() Controller {
	c, _ := p.actions.(Controller)
	return c
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
	if i := int(id) - cmdSourceBase; id >= cmdSourceBase && i < len(sourceToggles) {
		c := p.controller()
		if c == nil {
			return false
		}
		key := sourceToggles[i].Key
		// Flip what the menu showed, read afresh so a stale click cannot disagree with the config.
		c.SetSource(key, !c.MenuState().Sources[key])
		return true
	}
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
	var state *State
	if c := p.controller(); c != nil {
		st := c.MenuState()
		state = &st
	}
	for _, row := range buildMenu(p.language(), state) {
		appendMenuRow(menu, row)
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

func appendMenuRow(menu uintptr, row menuRow) {
	if row.Separator {
		procAppendMenuW.Call(menu, mfSeparator, 0, 0)
		return
	}
	text, err := syscall.UTF16PtrFromString(row.Label)
	if err != nil {
		return
	}
	flags := uintptr(mfString)
	if row.Disabled {
		flags |= mfGrayed | mfDisabled
	}
	if row.Checked {
		flags |= mfChecked
	}
	procAppendMenuW.Call(menu, flags, row.ID, uintptr(unsafe.Pointer(text)))
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
