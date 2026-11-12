//go:build windows

package tray

import (
	"runtime"
	"testing"
	"time"
)

func TestImageIconMatchesWin32Value(t *testing.T) {
	if imageIcon != 1 {
		t.Fatalf("IMAGE_ICON = %d, want 1", imageIcon)
	}
}

func TestWindowMessagePumpDispatchesTrayNotification(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	p, actions := newTestPanel()
	hwnd := createHostWindow(p)
	if hwnd == 0 {
		t.Fatal("createHostWindow failed")
	}
	p.hostHwnd.Store(hwnd)
	defer procDestroyWindow.Call(hwnd)

	threadID, _, _ := modKernel32.NewProc("GetCurrentThreadId").Call()
	postThreadMessage := modUser32.NewProc("PostThreadMessageW")
	go func() {
		time.Sleep(50 * time.Millisecond)
		procPostMessageW.Call(hwnd, wmAppTray, 0, wmLButtonUp)
		time.Sleep(50 * time.Millisecond)
		postThreadMessage.Call(threadID, wmQuit, 0, 0)
	}()
	p.messageLoop()
	if len(actions.fired) != 1 || actions.fired[0] != "show" {
		t.Fatalf("tray callback dispatched %v, want [show]", actions.fired)
	}
}

func TestWindowProcReadsMenuCommandFromWParam(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	p, actions := newTestPanel()
	hwnd := createHostWindow(p)
	if hwnd == 0 {
		t.Fatal("createHostWindow failed")
	}
	p.hostHwnd.Store(hwnd)
	defer procDestroyWindow.Call(hwnd)
	modUser32.NewProc("SendMessageW").Call(hwnd, wmCommand, cmdOpenPanel, 0)
	if len(actions.fired) != 1 || actions.fired[0] != "show" {
		t.Fatalf("WM_COMMAND dispatched %v, want [show]", actions.fired)
	}
}

func TestPopupMenuReturnCommandDispatches(t *testing.T) {
	p, actions := newTestPanel()
	if !p.dispatchMenuSelection(cmdQuit) {
		t.Fatal("popup command was not dispatched")
	}
	if len(actions.fired) != 1 || actions.fired[0] != "quit" {
		t.Fatalf("popup command dispatched %v, want [quit]", actions.fired)
	}
	if p.dispatchMenuSelection(0) {
		t.Fatal("dismissed popup was dispatched")
	}
}

func TestTrackPopupReturnsCommandFlag(t *testing.T) {
	if tpmReturnCmd != 0x0100 {
		t.Fatalf("TPM_RETURNCMD = %#x", tpmReturnCmd)
	}
}

type fakeActions struct {
	fired []string
}

func (f *fakeActions) ShowPanel() { f.fired = append(f.fired, "show") }
func (f *fakeActions) Quit()      { f.fired = append(f.fired, "quit") }

func newTestPanel() (*Panel, *fakeActions) {
	a := &fakeActions{}
	return New(a), a
}

func TestTrayClickOpensPanel(t *testing.T) {
	for _, ev := range []uintptr{wmLButtonUp, wmLButtonDblClk} {
		p, a := newTestPanel()
		if !p.Dispatch(wmAppTray, ev) {
			t.Fatalf("WM_APP mouse event 0x%x was not handled", ev)
		}
		if len(a.fired) != 1 || a.fired[0] != "show" {
			t.Fatalf("event 0x%x dispatched %v, want [show]", ev, a.fired)
		}
	}
}

func TestTrayMenuQuitStopsPanel(t *testing.T) {
	p, a := newTestPanel()
	if !p.Dispatch(wmCommand, cmdQuit) {
		t.Fatal("WM_COMMAND(cmdQuit) was not handled")
	}
	if len(a.fired) != 1 || a.fired[0] != "quit" {
		t.Fatalf("quit dispatched %v, want [quit]", a.fired)
	}
}

func TestTrayMenuOpenPanel(t *testing.T) {
	p, a := newTestPanel()
	if !p.Dispatch(wmCommand, cmdOpenPanel) {
		t.Fatal("WM_COMMAND(cmdOpenPanel) was not handled")
	}
	if len(a.fired) != 1 || a.fired[0] != "show" {
		t.Fatalf("open dispatched %v, want [show]", a.fired)
	}
}

func TestTrayIgnoresUnknownMessages(t *testing.T) {
	p, a := newTestPanel()
	if p.Dispatch(0x0084, 0) {
		t.Fatal("WM_NCHITTEST should not be intercepted")
	}
	if len(a.fired) != 0 {
		t.Fatalf("actions fired on unrelated message: %v", a.fired)
	}
}

func TestTrayTooltipShape(t *testing.T) {
	p, _ := newTestPanel()
	r := p.Recipe()
	if r.Tooltip != "Yozora" {
		t.Fatalf("tooltip = %q, want Yozora", r.Tooltip)
	}
	if r.IconID != iconResource {
		t.Fatalf("icon id = %d, want %d", r.IconID, iconResource)
	}
}

func TestMenuLabelsShape(t *testing.T) {
	items := menuItems
	if len(items) != 2 {
		t.Fatalf("menu has %d items, want 2", len(items))
	}
	if items[0].ID != cmdOpenPanel || items[1].ID != cmdQuit {
		t.Fatalf("menu ids = %d, %d", items[0].ID, items[1].ID)
	}
	if got := menuLabels("en"); got[0] != "Open control panel" || got[1] != "Quit Yozora" {
		t.Fatalf("English labels = %q", got)
	}
	if got := menuLabels("ja"); got[0] != "コントロールパネルを開く" || got[1] != "Yozora を終了" {
		t.Fatalf("Japanese labels = %q", got)
	}
	if got := menuLabels("fr"); got[0] != "Open control panel" {
		t.Fatalf("unknown language labels = %q", got)
	}
}

func TestMenuLanguageFollowsSettingThenWindows(t *testing.T) {
	const japanese, english, englishUK = 0x0411, 0x0409, 0x0809
	for _, c := range []struct {
		setting string
		ui      uint16
		want    string
	}{
		{"ja", english, "ja"},
		{"en", japanese, "en"},
		{"system", japanese, "ja"},
		{"system", englishUK, "en"},
		{"", japanese, "ja"},
		{"fr", japanese, "en"},
	} {
		if got := menuLanguage(c.setting, c.ui); got != c.want {
			t.Errorf("menuLanguage(%q, %#x) = %q, want %q", c.setting, c.ui, got, c.want)
		}
	}
}

func TestUnknownMenuCommandIsNotHandled(t *testing.T) {
	p, a := newTestPanel()
	if p.Dispatch(wmCommand, 4242) {
		t.Fatal("unknown menu command was handled")
	}
	if len(a.fired) != 0 {
		t.Fatalf("actions fired on unknown command: %v", a.fired)
	}
}

func TestMenuItemIDsAreUnique(t *testing.T) {
	seen := map[uintptr]bool{}
	for _, item := range menuItems {
		if item.ID == 0 || seen[item.ID] {
			t.Fatalf("menu id %d is zero or repeated", item.ID)
		}
		if item.Labels["en"] == "" || item.Labels["ja"] == "" || item.run == nil {
			t.Fatalf("menu item %d is incomplete", item.ID)
		}
		seen[item.ID] = true
	}
}

func TestPostCloseBeforeRunStopsTray(t *testing.T) {
	p, _ := newTestPanel()
	p.PostClose()
	done := make(chan error, 1)
	go func() { done <- p.Run() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run ignored a close requested before its window existed")
	}
	if p.HostHandle() != 0 {
		t.Fatal("host window survived Run")
	}
}

func TestRunStopsOnPostClose(t *testing.T) {
	p, _ := newTestPanel()
	done := make(chan error, 1)
	go func() { done <- p.Run() }()
	deadline := time.Now().Add(5 * time.Second)
	for p.HostHandle() == 0 {
		select {
		case err := <-done:
			t.Fatalf("Run returned early: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("host window never appeared")
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.PostClose()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop on PostClose")
	}
	if p.HostHandle() != 0 {
		t.Fatal("host window survived Run")
	}
}
