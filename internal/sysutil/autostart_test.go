//go:build windows

package sysutil

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestSplitRunCommand(t *testing.T) {
	cases := []struct{ in, exe, args string }{
		{`"C:\Program Files\Yozora\Yozora.exe" serve`, `C:\Program Files\Yozora\Yozora.exe`, "serve"},
		{`  "C:\Y\Yozora.exe"   serve  `, `C:\Y\Yozora.exe`, "serve"},
		{`C:\Y\Yozora.exe serve`, `C:\Y\Yozora.exe`, "serve"},
		{`"C:\Y\Yozora.exe"`, `C:\Y\Yozora.exe`, ""},
		{`"C:\Y\Yozora.exe`, `C:\Y\Yozora.exe`, ""},
		{``, ``, ""},
	}
	for _, c := range cases {
		exe, args := splitRunCommand(c.in)
		if exe != c.exe || args != c.args {
			t.Errorf("splitRunCommand(%q) = %q, %q; want %q, %q", c.in, exe, args, c.exe, c.args)
		}
	}
}

func TestAutostartNeedsRewrite(t *testing.T) {
	exe := `C:\Users\u\AppData\Local\Programs\Yozora\Yozora.exe`
	tmp := `C:\Users\u\AppData\Local\Temp`
	cases := []struct {
		name, registered, exe string
		want                  bool
	}{
		{"same path", autostartCommand(exe), exe, false},
		{"case differs only", `"c:\users\U\appdata\local\programs\yozora\YOZORA.EXE" serve`, exe, false},
		{"unclean but same", `"C:\Users\u\AppData\Local\Programs\Yozora\..\Yozora\Yozora.exe" serve`, exe, false},
		{"moved install", `"D:\Old\Yozora.exe" serve`, exe, true},
		{"old portable name", `"C:\Users\u\Downloads\uika-resonance.exe" serve`, exe, true},
		{"missing serve", `"` + exe + `"`, exe, true},
		{"other args", `"` + exe + `" setup`, exe, true},
		{"garbage", `not a command`, exe, true},
		{"temp binary never claims", `"D:\Old\Yozora.exe" serve`, tmp + `\go-build123\b001\exe\yozora.exe`, false},
		{"empty exe", `"D:\Old\Yozora.exe" serve`, "", false},
	}
	for _, c := range cases {
		if got := autostartNeedsRewrite(c.registered, c.exe, tmp); got != c.want {
			t.Errorf("%s: autostartNeedsRewrite = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestWithinDir(t *testing.T) {
	if !withinDir(`C:\Temp\a\b.exe`, `c:\temp`) {
		t.Error("child of temp not detected")
	}
	if withinDir(`C:\Temporary\b.exe`, `C:\Temp`) {
		t.Error("sibling with shared prefix treated as inside")
	}
	if withinDir(`C:\x.exe`, "") {
		t.Error("empty dir matched")
	}
}

// The registry round trip uses its own value name so it never touches a real
// UikaResonance entry on the machine running the tests.
func TestRepairAutostartRegistry(t *testing.T) {
	name := "YozoraAutostartTest" + strconv.Itoa(os.Getpid())
	t.Cleanup(func() { _ = deleteRunValue(name) })
	exe := `C:\New\Yozora.exe`
	tmp := t.TempDir()

	if rewrote, err := repairAutostart(name, exe, tmp); err != nil || rewrote {
		t.Fatalf("absent value: rewrote=%v err=%v; autostart must not be turned on", rewrote, err)
	}
	if _, ok := readRunValue(name); ok {
		t.Fatal("repair created a value that did not exist")
	}

	if err := writeRunValue(name, `"C:\Old\Yozora.exe" serve`); err != nil {
		t.Fatal(err)
	}
	if rewrote, err := repairAutostart(name, exe, tmp); err != nil || !rewrote {
		t.Fatalf("stale value: rewrote=%v err=%v", rewrote, err)
	}
	if got, _ := readRunValue(name); got != autostartCommand(exe) {
		t.Fatalf("value after repair = %q, want %q", got, autostartCommand(exe))
	}
	if rewrote, err := repairAutostart(name, exe, tmp); err != nil || rewrote {
		t.Fatalf("current value rewritten again: rewrote=%v err=%v", rewrote, err)
	}
	if rewrote, _ := repairAutostart(name, filepath.Join(tmp, "go-build", "yozora.exe"), tmp); rewrote {
		t.Fatal("a temp-dir binary repointed autostart")
	}
}

func TestReadRunValueLongPath(t *testing.T) {
	name := "YozoraAutostartLongTest" + strconv.Itoa(os.Getpid())
	t.Cleanup(func() { _ = deleteRunValue(name) })
	long := `C:\`
	for len(long) < 900 {
		long += `deep\`
	}
	cmd := autostartCommand(long + "Yozora.exe")
	if err := writeRunValue(name, cmd); err != nil {
		t.Fatal(err)
	}
	if got, ok := readRunValue(name); !ok || got != cmd {
		t.Fatalf("long value read back ok=%v len=%d, want len=%d", ok, len(got), len(cmd))
	}
}
