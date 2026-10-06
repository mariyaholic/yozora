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
	p.hostHwnd = hwnd
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
	p.hostHwnd = hwnd
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
	items := menuLabels()
	if len(items) != 2 {
		t.Fatalf("menu has %d items, want 2", len(items))
	}
	if items[0].ID != cmdOpenPanel || items[0].Label != "Open control panel" {
		t.Fatalf("first item = %+v", items[0])
	}
	if items[1].ID != cmdQuit || items[1].Label != "Quit Yozora" {
		t.Fatalf("second item = %+v", items[1])
	}
}
