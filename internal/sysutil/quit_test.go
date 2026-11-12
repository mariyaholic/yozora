//go:build windows

package sysutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// StopRunning posts the tray's own Quit command; this keeps the mirrored
// constants honest if internal/tray ever renames its class or renumbers it.
func TestTrayConstantsMatchTrayPackage(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "tray", "tray.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`trayClassName\s*=\s*"` + trayHostClass + `"`).Match(src) {
		t.Errorf("internal/tray trayClassName no longer %q", trayHostClass)
	}
	if !regexp.MustCompile(`cmdQuit\s*=\s*1002\b`).Match(src) {
		t.Errorf("internal/tray cmdQuit no longer %d", trayCmdQuit)
	}
}

func TestStopRunningWithNothingRunning(t *testing.T) {
	start := time.Now()
	if err := StopRunning(filepath.Join(t.TempDir(), "missing.json"), 3*time.Second); err != nil {
		t.Fatalf("StopRunning with nothing running: %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("StopRunning waited %v with nothing to stop", d)
	}
}

// The end-to-end tests below re-run this test binary as a stand-in panel
// (a window of a private class that exits on the tray Quit command) and as a
// stand-in daemon, so the image-path guard sees the same executable.

const fakeTrayClass = "YozoraTrayHostTest"

var (
	procRegisterClassExW = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW  = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW   = modUser32.NewProc("DefWindowProcW")
	procGetMessageW      = modUser32.NewProc("GetMessageW")
	procDispatchMessageW = modUser32.NewProc("DispatchMessageW")
	procPostQuitMessage  = modUser32.NewProc("PostQuitMessage")
)

type testWndClass struct {
	cbSize, style                    uint32
	wndProc                          uintptr
	clsExtra, wndExtra               int32
	instance, icon, cursor, bg, menu uintptr
	className                        *uint16
	iconSm                           uintptr
}

func runFakeTray() {
	runtime.LockOSThread()
	inst, _, _ := modKernel32.NewProc("GetModuleHandleW").Call(0)
	class, _ := syscall.UTF16PtrFromString(fakeTrayClass)
	wc := testWndClass{className: class, wndProc: syscall.NewCallback(func(h, msg, wp, lp uintptr) uintptr {
		if msg == wmCommand && wp&0xffff == trayCmdQuit {
			procPostQuitMessage.Call(0)
			return 0
		}
		r, _, _ := procDefWindowProcW.Call(h, msg, wp, lp)
		return r
	})}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	wc.instance = inst
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if h, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, 0, 0, inst, 0); h == 0 {
		os.Exit(3)
	}
	var m [64]byte
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m[0])), 0, 0, 0)
		if r == 0 || r == ^uintptr(0) {
			os.Exit(0)
		}
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m[0])))
	}
}

func TestMain(m *testing.M) {
	switch os.Getenv("YOZORA_SYSUTIL_HELPER") {
	case "tray":
		runFakeTray()
	case "daemon":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// startHelper starts a stand-in process; exited closes when it ends.
func startHelper(t *testing.T, role string) (cmd *exec.Cmd, exited <-chan struct{}) {
	t.Helper()
	cmd = exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "YOZORA_SYSUTIL_HELPER="+role)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	return cmd, done
}

func TestStopRunningQuitsTrayAndDaemon(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tray, trayExited := startHelper(t, "tray")
	for i := 0; findWindowByClass(fakeTrayClass) == 0; i++ {
		select {
		case <-trayExited:
			// Headless Wine cannot create windows; Windows CI always can.
			t.Skip("cannot create a window in this environment")
		default:
		}
		if i > 50 {
			t.Fatal("fake tray window never appeared")
		}
		time.Sleep(100 * time.Millisecond)
	}
	daemon, _ := startHelper(t, "daemon")
	endpoint := filepath.Join(t.TempDir(), "dashboard.json")
	if err := os.WriteFile(endpoint, []byte(`{"pid":`+strconv.Itoa(daemon.Process.Pid)+`}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !daemonRunning(endpoint, exe) {
		t.Fatal("stand-in daemon not recognised as running")
	}
	if err := stopRunning(endpoint, exe, fakeTrayClass, 5*time.Second); err != nil {
		t.Fatalf("stopRunning: %v", err)
	}
	if pidAlive(tray.Process.Pid) || pidAlive(daemon.Process.Pid) {
		t.Fatal("a stand-in process survived stopRunning")
	}
}

func TestStopRunningLeavesOtherExecutables(t *testing.T) {
	daemon, _ := startHelper(t, "daemon")
	endpoint := filepath.Join(t.TempDir(), "dashboard.json")
	if err := os.WriteFile(endpoint, []byte(`{"pid":`+strconv.Itoa(daemon.Process.Pid)+`}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := stopRunning(endpoint, `C:\elsewhere\Yozora.exe`, fakeTrayClass, time.Second); err != nil {
		t.Fatalf("stopRunning: %v", err)
	}
	if !pidAlive(daemon.Process.Pid) {
		t.Fatal("stopRunning killed a process from another executable")
	}
}
