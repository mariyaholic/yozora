//go:build windows

package tray

import (
	"testing"
)

// fakeActions records which panel actions fired, in order.
type fakeActions struct {
	fired []string
}

func (f *fakeActions) ShowPanel() { f.fired = append(f.fired, "show") }
func (f *fakeActions) Quit()      { f.fired = append(f.fired, "quit") }

func newTestPanel() (*Panel, *fakeActions) {
	a := &fakeActions{}
	return New(a), a
}

// TestTrayClickOpensPanel: a synthetic left-button tray notification
// (single click and double click both lift the panel) triggers ShowPanel.
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

// TestTrayMenuQuitStopsPanel: selecting "Quit Yozora" dispatches Quit.
func TestTrayMenuQuitStopsPanel(t *testing.T) {
	p, a := newTestPanel()
	if !p.Dispatch(wmCommand, cmdQuit) {
		t.Fatal("WM_COMMAND(cmdQuit) was not handled")
	}
	if len(a.fired) != 1 || a.fired[0] != "quit" {
		t.Fatalf("quit dispatched %v, want [quit]", a.fired)
	}
}

// TestTrayMenuOpenPanel: selecting "Open control panel" dispatches ShowPanel.
func TestTrayMenuOpenPanel(t *testing.T) {
	p, a := newTestPanel()
	if !p.Dispatch(wmCommand, cmdOpenPanel) {
		t.Fatal("WM_COMMAND(cmdOpenPanel) was not handled")
	}
	if len(a.fired) != 1 || a.fired[0] != "show" {
		t.Fatalf("open dispatched %v, want [show]", a.fired)
	}
}

// TestTrayIgnoresUnknownMessages: unrelated messages fall through untouched.
func TestTrayIgnoresUnknownMessages(t *testing.T) {
	p, a := newTestPanel()
	if p.Dispatch(0x0084 /*WM_NCHITTEST*/, 0) {
		t.Fatal("WM_NCHITTEST should not be intercepted")
	}
	if len(a.fired) != 0 {
		t.Fatalf("actions fired on unrelated message: %v", a.fired)
	}
}

// TestTrayTooltipShape: the notify icon this package registers carries the
// expected tooltip and the shared icon resource.
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

// TestMenuLabelsShape: the right-click popup lists exactly the two entries,
// in the expected order with the expected wording.
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
