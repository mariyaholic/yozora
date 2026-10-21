//go:build windows

package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2/pkg/edge"
)

func TestFirstTrayQuitRequestRunsShutdownPath(t *testing.T) {
	dir := t.TempDir()
	a := &panelTrayActions{
		dashboardJSON: filepath.Join(dir, "missing-dashboard.json"),
		dataDir:       dir,
	}
	a.Quit()
	b, err := os.ReadFile(filepath.Join(dir, "launcher.log"))
	if err != nil {
		t.Fatalf("first quit request skipped shutdown path: %v", err)
	}
	if !strings.Contains(string(b), "tray quit: stop daemon:") {
		t.Fatalf("shutdown error not recorded: %s", b)
	}
}

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

func TestPanelLifecycle(t *testing.T) {
	for _, mode := range []string{"reopen", "reopen-launch", "quit-closed", "tray-not-ready", "tray-failure-open", "tray-exit-closed", "reopen-teardown", "reopen-external-teardown", "no-entry"} {
		t.Run(mode, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			dir := t.TempDir()
			a := &panelTrayActions{dataDir: dir, dashboardJSON: filepath.Join(dir, "missing-dashboard.json")}
			if mode != "no-entry" {
				a.initEntry()
				if a.entry == 0 {
					t.Fatal("entry creation failed")
				}
			}
			defer procDestroyPanelWindow.Call(a.entry)
			a.trayReady.Store(mode != "tray-not-ready")
			var views []*fakePanelView
			var asyncDone chan struct{}
			factory := func() panelView {
				v := &fakePanelView{hwnd: testPanelHWND(t)}
				views = append(views, v)
				if len(views) == 1 && (mode == "reopen-teardown" || mode == "reopen-external-teardown") {
					v.onTeardown = func() {
						if mode == "reopen-teardown" {
							a.ShowPanel()
						} else {
							// A second process cannot set the in-process flag.
							procPostPanelMessage.Call(a.entry, panelReopenMessage, 0, 0)
						}
						// Model the library dispatching the request during teardown.
						var msg panelMessage
						procGetPanelMessage.Call(uintptr(unsafe.Pointer(&msg)), a.entry, panelReopenMessage, panelReopenMessage)
						procDispatchPanelMessage.Call(uintptr(unsafe.Pointer(&msg)))
					}
				}
				v.run = func() {
					hwnd := uintptr(v.hwnd)
					prop, _ := syscall.UTF16PtrFromString(panelHWNDProp)
					got, _, _ := procGetPropW.Call(a.entry, uintptr(unsafe.Pointer(prop)))
					if a.entry != 0 && got != hwnd {
						t.Fatalf("entry prop = %x, want %x", got, hwnd)
					}
					if mode == "tray-failure-open" {
						a.trayStopped()
						// Dispatch all pending messages: tray failure must not post WM_CLOSE.
						var msg panelMessage
						peek := syscall.NewLazyDLL("user32.dll").NewProc("PeekMessageW")
						for {
							r, _, _ := peek.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 1)
							if r == 0 {
								break
							}
							procDispatchPanelMessage.Call(uintptr(unsafe.Pointer(&msg)))
						}
						if v.closed != 0 {
							t.Fatal("tray failure closed live panel")
						}
					}
					syscall.NewLazyDLL("user32.dll").NewProc("SendMessageW").Call(hwnd, 0x0010, 0, 0)
					if live, _, _ := procIsPanelWindow.Call(hwnd); live != 0 {
						t.Fatal("closed panel was not destroyed")
					}
					if v.closed != 1 {
						t.Fatalf("teardown count = %d", v.closed)
					}
					if len(views) == 1 {
						switch mode {
						case "reopen":
							a.ShowPanel()
						case "reopen-launch":
							activateRunningPanel()
						case "quit-closed":
							asyncDone = make(chan struct{})
							go func() { defer close(asyncDone); a.Quit() }()
						case "tray-exit-closed":
							asyncDone = make(chan struct{})
							go func() { defer close(asyncDone); a.trayStopped() }()
						}
					} else {
						a.quitRequested.Store(true)
						a.closePanel()
					}
				}
				return v
			}
			if !runPanelLifecycle(a, "about:blank", factory) {
				t.Fatal("lifecycle failed")
			}
			if asyncDone != nil {
				<-asyncDone
			}
			want := 1
			if strings.HasPrefix(mode, "reopen") {
				want = 2
			}
			if len(views) != want {
				t.Fatalf("created %d views, want %d", len(views), want)
			}
			for _, v := range views {
				if v.closed != 1 {
					t.Fatalf("teardown called %d times", v.closed)
				}
			}
		})
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

func TestReopenDuringFactoryIsSatisfiedByNewView(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dir := t.TempDir()
	a := &panelTrayActions{dataDir: dir, dashboardJSON: filepath.Join(dir, "missing-dashboard.json")}
	a.initEntry()
	defer procDestroyPanelWindow.Call(a.entry)
	a.trayReady.Store(true)
	var views int
	factory := func() panelView {
		views++
		v := &fakePanelView{hwnd: testPanelHWND(t)}
		if views == 1 {
			// A second Yozora process launches while the WebView2 factory is
			// pumping messages on this thread (before a.panel is published).
			procPostPanelMessage.Call(a.entry, panelReopenMessage, 0, 0)
			var msg panelMessage
			procGetPanelMessage.Call(uintptr(unsafe.Pointer(&msg)), a.entry, panelReopenMessage, panelReopenMessage)
			procDispatchPanelMessage.Call(uintptr(unsafe.Pointer(&msg)))
		}
		v.run = func() {
			syscall.NewLazyDLL("user32.dll").NewProc("SendMessageW").Call(uintptr(v.hwnd), 0x0010, 0, 0)
			if views >= 2 {
				a.quitRequested.Store(true)
				a.closePanel()
			}
		}
		return v
	}
	go func() { time.Sleep(400 * time.Millisecond); a.Quit() }()
	runPanelLifecycle(a, "about:blank", factory)
	if views != 1 {
		t.Fatalf("spurious reopen: views=%d want 1", views)
	}
}
