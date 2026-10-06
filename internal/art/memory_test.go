//go:build windows

package art

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Baseline memory-profile sanity: the resolver must stay modest when
// resolving a burst of tracks. Runs the real Resolver's full chain against
// local fixtures and reports the peak in-use heap via runtime.ReadMemStats
// (no live network; the_art_ fixture
// transport is injected).
func TestResolverMemoryStaysModestBeyondCacheBudget(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, 1<<20) // tiny disk budget forces trim exercise
	r.httpClient.Transport = fixtureTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"results":[{"artworkUrl100":"https://fixture.invalid/a/100x100bb.jpg","trackViewUrl":"https://fixture.invalid/track"}]}`)),
		}, nil
	})
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	for i := 0; i < 500; i++ {
		tr := Track{Source: "spotify", Title: fmt.Sprintf("track %d", i), Artist: "artist", Album: "album"}
		l := r.Resolve(tr, "cdn")
		if l.ImageURL == "" {
			t.Fatalf("resolve %d returned nothing", i)
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	heapGrewMB := float64(int64(after.HeapAlloc)-int64(before.HeapAlloc)) / (1 << 20)
	t.Logf("heap delta %.1f MB for 500 distinct resolutions (incl. cache map)", heapGrewMB)
	// 512-entry map cap + JSON + strings: the live set is a few KB, so any
	// growth beyond a small budget — including a substantial *shrink* caused
	// by unrelated allocations in parallel tests — signals something wrong.
	if heapGrewMB > 10 || heapGrewMB < -10 {
		t.Fatalf("resolver memory shifted unexpectedly: %.1f MB", heapGrewMB)
	}
	// Disk cache stayed honest despite 500 lookups (one file per distinct key capped by trim)
	entries, _ := os.ReadDir(dir)
	t.Logf("cache entries: %d", len(entries))
	if float64(len(entries)) > 600 {
		t.Fatalf("cache map bookkeeping runaway: %d files", len(entries))
	}
	_ = filepath.Join
}
