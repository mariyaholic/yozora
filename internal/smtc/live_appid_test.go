//go:build windows

package smtc

import (
	"fmt"
	"os"
	"testing"
)

// Opt-in live diagnostic: prints the SourceAppUserModelId of every SMTC
// session (and whether a thumbnail entry exists) without logging thumbnails
// or identifiers. Skipped unless YOZORA_READONLY_SMTC_PROBE=1 is set; never
// touches playback.
func TestLiveSessionAppIDDump(t *testing.T) {
	if os.Getenv("YOZORA_READONLY_SMTC_PROBE") != "1" {
		t.Skip("set YOZORA_READONLY_SMTC_PROBE=1 to run")
	}
	m, err := Connect()
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer m.Close()
	tracks, err := m.Sessions()
	if err != nil {
		t.Fatalf("sessions: %v", err)
	}
	for i, tr := range tracks {
		fmt.Printf("session %d: appID=%q classified=%q thumb_hook=%v title_len=%d\n",
			i, tr.AppID, Classify(tr.AppID), tr.Thumb != nil, len(tr.Title))
	}
}
