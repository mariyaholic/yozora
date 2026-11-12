//go:build windows

package cli

import (
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2/pkg/edge"
)

type fakePanelView struct {
	hwnd       unsafe.Pointer
	closed     int
	run        func()
	onTeardown func()
}

func (v *fakePanelView) Navigate(string)        {}
func (v *fakePanelView) Window() unsafe.Pointer { return v.hwnd }
func (v *fakePanelView) Run()                   { v.run() }
func (v *fakePanelView) teardown() {
	v.closed++
	if v.onTeardown != nil {
		v.onTeardown()
	}
}

func testPanelHWND(t *testing.T) unsafe.Pointer {
	t.Helper()
	class, _ := syscall.UTF16PtrFromString("Static")
	hwnd, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	if hwnd == 0 {
		t.Fatal("test window creation failed")
	}
	// Preserve the handle bits without an unsafe uintptr-to-pointer conversion.
	return *(*unsafe.Pointer)(unsafe.Pointer(&hwnd))
}

// pumpUntilDestroyed models the WebView2 message loop: it dispatches this
// thread's messages until hwnd is gone.
func pumpUntilDestroyed(t *testing.T, hwnd uintptr) {
	t.Helper()
	peek := syscall.NewLazyDLL("user32.dll").NewProc("PeekMessageW")
	dispatch := syscall.NewLazyDLL("user32.dll").NewProc("DispatchMessageW")
	var msg [48]byte
	deadline := time.Now().Add(3 * time.Second)
	for {
		if live, _, _ := procIsPanelWindow.Call(hwnd); live == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("panel window was never closed")
		}
		if r, _, _ := peek.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0, 1); r != 0 {
			dispatch.Call(uintptr(unsafe.Pointer(&msg[0])))
			continue
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPanelLifecycle(t *testing.T) {
	for _, mode := range []string{"user-close", "focus-running", "quit-open", "quit-during-factory", "no-entry"} {
		t.Run(mode, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			a := &panelSession{dataDir: t.TempDir()}
			if mode != "no-entry" {
				a.initEntry()
				if a.entry == 0 {
					t.Fatal("entry creation failed")
				}
				defer procDestroyPanelWindow.Call(a.entry)
			}
			var views []*fakePanelView
			factory := func() panelView {
				v := &fakePanelView{hwnd: testPanelHWND(t)}
				views = append(views, v)
				if mode == "quit-during-factory" {
					a.Quit()
				}
				v.run = func() {
					hwnd := uintptr(v.hwnd)
					if a.entry != 0 {
						prop, _ := syscall.UTF16PtrFromString(panelHWNDProp)
						got, _, _ := procGetPropW.Call(a.entry, uintptr(unsafe.Pointer(prop)))
						if got != hwnd {
							t.Fatalf("entry prop = %x, want %x", got, hwnd)
						}
					}
					switch mode {
					case "focus-running":
						if !focusRunningPanel() {
							t.Fatal("a second launch could not find the open panel")
						}
						fallthrough
					case "user-close", "no-entry":
						syscall.NewLazyDLL("user32.dll").NewProc("SendMessageW").Call(hwnd, 0x0010, 0, 0)
					case "quit-open":
						done := make(chan struct{})
						go func() { defer close(done); a.Quit() }()
						<-done
						pumpUntilDestroyed(t, hwnd)
					case "quit-during-factory":
						pumpUntilDestroyed(t, hwnd)
					}
					if live, _, _ := procIsPanelWindow.Call(hwnd); live != 0 {
						t.Fatal("closed panel was not destroyed")
					}
					if v.closed != 1 {
						t.Fatalf("teardown count = %d", v.closed)
					}
				}
				return v
			}
			if !runPanelLifecycle(a, "about:blank", factory) {
				t.Fatal("lifecycle failed")
			}
			if len(views) != 1 {
				t.Fatalf("created %d views, want 1", len(views))
			}
			if views[0].closed != 1 {
				t.Fatalf("teardown called %d times", views[0].closed)
			}
			if a.entry != 0 && focusRunningPanel() {
				t.Fatal("a closed panel still reports as focusable")
			}
		})
	}
}

func TestQuitBeforeFactorySkipsWindow(t *testing.T) {
	a := &panelSession{dataDir: t.TempDir()}
	a.Quit()
	called := false
	if !runPanelLifecycle(a, "about:blank", func() panelView { called = true; return nil }) {
		t.Fatal("quit before open fell back to the browser")
	}
	if called {
		t.Fatal("window created after quit")
	}
}

func TestWatchDaemonExitFiresWhenDaemonEnds(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "ping -n 30 127.0.0.1 >nul")
	if err := cmd.Start(); err != nil {
		t.Skipf("cmd unavailable: %v", err)
	}
	defer cmd.Process.Kill()
	done := make(chan struct{})
	go watchDaemonExit(cmd.Process.Pid, t.TempDir(), func() { close(done) })
	select {
	case <-done:
		t.Fatal("watcher fired while the daemon was running")
	case <-time.After(200 * time.Millisecond):
	}
	_ = cmd.Process.Kill()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not report the daemon's exit")
	}
	_ = cmd.Wait()
}

func TestClaimPanel(t *testing.T) {
	released := 0
	owned := func() (bool, func(), error) { return true, func() { released++ }, nil }
	held := func() (bool, func(), error) { return false, func() {}, nil }

	owner, release, err := claimPanel(owned, func() bool { t.Fatal("focused while owner"); return false }, time.Second, time.Millisecond)
	if err != nil || !owner {
		t.Fatalf("owner = %v, err = %v", owner, err)
	}
	release()
	if released != 1 {
		t.Fatal("release not returned")
	}

	focused := 0
	owner, _, _ = claimPanel(held, func() bool { focused++; return true }, time.Second, time.Millisecond)
	if owner || focused != 1 {
		t.Fatalf("open panel: owner = %v, focus calls = %d", owner, focused)
	}

	// The previous panel is closing: its mutex frees after a few tries.
	tries := 0
	closing := func() (bool, func(), error) {
		tries++
		if tries < 3 {
			return false, func() {}, nil
		}
		return true, func() {}, nil
	}
	owner, _, _ = claimPanel(closing, func() bool { return false }, time.Second, time.Millisecond)
	if !owner || tries != 3 {
		t.Fatalf("handoff: owner = %v after %d tries", owner, tries)
	}

	owner, _, _ = claimPanel(held, func() bool { return false }, 20*time.Millisecond, time.Millisecond)
	if owner {
		t.Fatal("claimed a mutex that never freed")
	}
}

func TestChromiumFromView(t *testing.T) {
	if _, ok := chromiumFromView(&struct{}{}); ok {
		t.Fatal("accepted missing browser")
	}
	browser := edge.NewChromium()
	local := &struct {
		browser interface{ Embed(uintptr) bool }
	}{browser}
	if got, ok := chromiumFromView(local); !ok || got != browser {
		t.Fatal("browser not extracted")
	}
	if _, ok := chromiumFromView(&struct{ browser string }{"wrong"}); ok {
		t.Fatal("accepted incompatible browser")
	}
}
