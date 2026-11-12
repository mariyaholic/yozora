// Package logfile is a size-capped log writer. The daemon runs for weeks at a
// time, so checking the size only at startup lets the log grow without bound.
package logfile

import (
	"os"
	"strings"
	"sync"
)

// Writer appends to a file and, when a write would take it past Max bytes,
// renames it to path+".1" (replacing the previous one) and starts a new file.
// At most about 2*Max bytes are ever on disk.
type Writer struct {
	path string
	max  int64

	mu   sync.Mutex
	f    *os.File
	size int64
}

// Open opens path for appending. An existing file already over max is rotated
// first, so a crash loop cannot grow it either.
func Open(path string, max int64) (*Writer, error) {
	w := &Writer{path: path, max: max}
	if st, err := os.Stat(path); err == nil && st.Size() > max {
		_ = os.Rename(path, path+".1")
	}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.f, w.size = f, st.Size()
	return nil
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f != nil && w.size > 0 && w.size+int64(len(p)) > w.max {
		w.rotate()
	}
	if w.f == nil {
		// A failed reopen is retried on the next line rather than losing logging for good.
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// rotate must leave a usable file behind. If the rename fails (another program
// holding the old file open, say), writing simply continues in the current file
// and the next write tries again.
func (w *Writer) rotate() {
	w.f.Close()
	w.f = nil
	if err := os.Rename(w.path, w.path+".1"); err != nil {
		_ = w.open()
		return
	}
	_ = w.open()
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// Levels lists the accepted values of the log.level setting.
var Levels = []string{"debug", "info", "warn", "error", "off"}

// Level is a parsed log.level setting.
type Level struct {
	// File reports whether a log file is written at all ("off" disables it).
	File bool
	// Source adds file:line to every entry ("debug").
	Source bool
}

// ParseLevel reads a log.level value. Yozora's log lines carry no severity, so
// "warn" and "error" currently write the same lines as "info". Unknown values
// fall back to "info" and report ok=false so the caller can say so.
func ParseLevel(s string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return Level{File: true, Source: true}, true
	case "", "info", "warn", "error":
		return Level{File: true}, true
	case "off":
		return Level{}, true
	default:
		return Level{File: true}, false
	}
}
