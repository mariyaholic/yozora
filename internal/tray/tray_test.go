//go:build windows

package tray

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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

// fakeController adds the richer menu's state and calls to fakeActions.
type fakeController struct {
	fakeActions
	state State
}

func (f *fakeController) MenuState() State         { return f.state }
func (f *fakeController) PauseFor(d time.Duration) { f.fired = append(f.fired, "pause:"+d.String()) }
func (f *fakeController) PauseUntilResumed()       { f.fired = append(f.fired, "pause:open") }
func (f *fakeController) Resume()                  { f.fired = append(f.fired, "resume") }
func (f *fakeController) SetSource(key string, on bool) {
	f.fired = append(f.fired, fmt.Sprintf("source:%s=%v", key, on))
}

func labelsOf(rows []menuRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		switch {
		case r.Separator:
			out[i] = "-"
		case r.Checked:
			out[i] = "[x] " + r.Label
		case r.Disabled:
			out[i] = "(" + r.Label + ")"
		default:
			out[i] = r.Label
		}
	}
	return out
}

func TestBuildMenuWithoutControllerKeepsFixedRows(t *testing.T) {
	got := labelsOf(buildMenu("en", nil))
	if !reflect.DeepEqual(got, []string{"Open control panel", "Quit Yozora"}) {
		t.Fatalf("menu = %q", got)
	}
}

func TestBuildMenuStates(t *testing.T) {
	playing := State{Title: "Let Down", Artist: "Radiohead", Sources: map[string]bool{"youtube": true, "twitch": false, "youtube_tabs": true}}
	paused := State{Paused: true, Sources: map[string]bool{}}
	for _, c := range []struct {
		name string
		lang string
		st   State
		want []string
	}{
		{"playing en", "en", playing, []string{"(Let Down \u2013 Radiohead)", "Open control panel", "Pause sharing for 1 hour", "Pause until I resume",
			"-", "[x] YouTube", "Twitch", "[x] Background Edge tabs", "-", "Quit Yozora"}},
		{"paused en", "en", paused, []string{"(Nothing playing)", "Open control panel", "Resume sharing",
			"-", "YouTube", "Twitch", "Background Edge tabs", "-", "Quit Yozora"}},
		{"playing ja", "ja", playing, []string{"(Let Down \u2013 Radiohead)", "コントロールパネルを開く", "共有を 1 時間一時停止", "再開するまで一時停止",
			"-", "[x] YouTube", "Twitch", "[x] バックグラウンドの Edge タブ", "-", "Yozora を終了"}},
		{"paused ja", "ja", paused, []string{"(再生中の曲はありません)", "コントロールパネルを開く", "共有を再開",
			"-", "YouTube", "Twitch", "バックグラウンドの Edge タブ", "-", "Yozora を終了"}},
	} {
		st := c.st
		if got := labelsOf(buildMenu(c.lang, &st)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestBuildMenuRowIDsAreDistinct(t *testing.T) {
	st := State{}
	seen := map[uintptr]bool{}
	for _, r := range buildMenu("en", &st) {
		if r.Separator {
			continue
		}
		if seen[r.ID] {
			t.Fatalf("id %d repeated", r.ID)
		}
		seen[r.ID] = true
	}
}

func TestHeaderLabelTruncatesAndTidies(t *testing.T) {
	if got := headerLabel("en", State{Title: "Solo"}); got != "Solo" {
		t.Fatalf("title only = %q", got)
	}
	if got := headerLabel("en", State{Title: "  Spaced \n Out ", Artist: " A  "}); got != "Spaced Out \u2013 A" {
		t.Fatalf("whitespace = %q", got)
	}
	long := headerLabel("en", State{Title: strings.Repeat("a", 80), Artist: "b"})
	if r := []rune(long); len(r) != maxHeaderRunes || r[len(r)-1] != '\u2026' {
		t.Fatalf("long = %q (%d runes)", long, len(r))
	}
	jp := headerLabel("ja", State{Title: strings.Repeat("あ", 80)})
	if r := []rune(jp); len(r) != maxHeaderRunes || !utf8.ValidString(jp) {
		t.Fatalf("multibyte truncation = %q (%d runes)", jp, len(r))
	}
	if got := headerLabel("en", State{Artist: "Only artist"}); got != "Nothing playing" {
		t.Fatalf("no title = %q", got)
	}
}

func TestControllerMenuCommands(t *testing.T) {
	c := &fakeController{state: State{Sources: map[string]bool{"youtube": true, "twitch": false}}}
	p := New(c)
	for _, id := range []uintptr{cmdPauseHour, cmdPauseOpen, cmdResume, cmdSourceBase, cmdSourceBase + 1} {
		if !p.Dispatch(wmCommand, id) {
			t.Fatalf("command %d not handled", id)
		}
	}
	want := []string{"pause:1h0m0s", "pause:open", "resume", "source:youtube=false", "source:twitch=true"}
	if !reflect.DeepEqual(c.fired, want) {
		t.Fatalf("fired %q, want %q", c.fired, want)
	}
	if p.Dispatch(wmCommand, cmdSourceBase+uintptr(len(sourceToggles))) {
		t.Fatal("a toggle past the table was handled")
	}
	// Without a controller the pause and toggle commands do nothing, and nothing panics.
	plain, a := newTestPanel()
	plain.Dispatch(wmCommand, cmdPauseHour)
	if plain.Dispatch(wmCommand, cmdSourceBase) || len(a.fired) != 0 {
		t.Fatalf("plain actions fired %v", a.fired)
	}
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
	index := map[uintptr]int{}
	for i, item := range menuItems {
		index[item.ID] = i
	}
	open, quit := index[cmdOpenPanel], index[cmdQuit]
	if got := menuLabels("en"); got[open] != "Open control panel" || got[quit] != "Quit Yozora" {
		t.Fatalf("English labels = %q", got)
	}
	if got := menuLabels("ja"); got[open] != "コントロールパネルを開く" || got[quit] != "Yozora を終了" {
		t.Fatalf("Japanese labels = %q", got)
	}
	if got := menuLabels("fr"); got[open] != "Open control panel" {
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
