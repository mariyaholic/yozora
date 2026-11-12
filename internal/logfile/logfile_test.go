package logfile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWriterRotatesWhileRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	w, err := Open(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	line := bytes.Repeat([]byte("a"), 39)
	line = append(line, '\n') // 40 bytes
	for range 2 {
		if _, err := w.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatal("rotated before the cap was reached")
	}
	// 80 + 40 > 100: this write goes to a fresh file.
	marker := append(bytes.Repeat([]byte("b"), 39), '\n')
	if _, err := w.Write(marker); err != nil {
		t.Fatal(err)
	}
	old, _ := os.ReadFile(path + ".1")
	cur, _ := os.ReadFile(path)
	if len(old) != 80 || !bytes.Equal(cur, marker) {
		t.Fatalf("old=%d bytes cur=%q", len(old), cur)
	}

	// Only one old file is kept: the second of these writes rotates again and
	// replaces it.
	for range 2 {
		if _, err := w.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	old, _ = os.ReadFile(path + ".1")
	cur, _ = os.ReadFile(path)
	if !bytes.HasPrefix(old, marker) || len(old) != 80 || len(cur) != 40 {
		t.Fatalf("second rotation: old=%d cur=%d", len(old), len(cur))
	}
	matches, _ := filepath.Glob(path + "*")
	if len(matches) != 2 {
		t.Fatalf("files on disk: %v", matches)
	}
}

func TestOpenRotatesOversizedFileAndAppendsOtherwise(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.log")
	if err := os.WriteFile(big, bytes.Repeat([]byte("x"), 200), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := Open(big, 100)
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	if st, err := os.Stat(big + ".1"); err != nil || st.Size() != 200 {
		t.Fatal("oversized log not rotated at open")
	}

	small := filepath.Join(dir, "small.log")
	if err := os.WriteFile(small, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err = Open(small, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("new\n")); err != nil {
		t.Fatal(err)
	}
	w.Close()
	if b, _ := os.ReadFile(small); string(b) != "old\nnew\n" {
		t.Fatalf("existing log not appended to: %q", b)
	}
}

func TestOversizedSingleWriteStillLands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	w, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	long := bytes.Repeat([]byte("z"), 50)
	if _, err := w.Write(long); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, long) {
		t.Fatal("a line longer than the cap was dropped")
	}
}

func TestParseLevel(t *testing.T) {
	for _, tc := range []struct {
		in         string
		file, src  bool
		recognized bool
	}{
		{"info", true, false, true},
		{"", true, false, true},
		{" DEBUG ", true, true, true},
		{"warn", true, false, true},
		{"error", true, false, true},
		{"off", false, false, true},
		{"verbose", true, false, false},
	} {
		lv, ok := ParseLevel(tc.in)
		if lv.File != tc.file || lv.Source != tc.src || ok != tc.recognized {
			t.Errorf("ParseLevel(%q) = %+v, %v", tc.in, lv, ok)
		}
	}
}
