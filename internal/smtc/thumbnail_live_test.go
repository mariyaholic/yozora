//go:build windows

package smtc

import (
	"os"
	"testing"
)

// Opt-in bounded, read-only diagnostic: never logs session identifiers/titles,
// changes playback, or publishes activity. Not part of ordinary fixture tests.
func TestLiveBrowserThumbnail(t *testing.T) {
	if os.Getenv("YOZORA_READONLY_THUMB_PROBE") != "1" {
		t.Skip("read-only live probe is opt-in")
	}
	m, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	tracks, err := m.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, tr := range tracks {
		if Classify(tr.AppID) != "browser" {
			continue
		}
		found++
		if tr.Thumb == nil {
			t.Log("browser thumbnail reference absent")
			continue
		}
		data, err := tr.Thumb()
		t.Logf("browser thumbnail bytes=%d error=%v", len(data), err)
	}
	t.Logf("browser sessions=%d", found)
}
