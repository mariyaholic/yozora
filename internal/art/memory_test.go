//go:build windows

package art

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestResolverMemoryStaysModestBeyondCacheBudget(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, 1<<20)
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

	if heapGrewMB > 10 || heapGrewMB < -10 {
		t.Fatalf("resolver memory shifted unexpectedly: %.1f MB", heapGrewMB)
	}

	entries, _ := os.ReadDir(dir)
	t.Logf("cache entries: %d", len(entries))
	if float64(len(entries)) > 600 {
		t.Fatalf("cache map bookkeeping runaway: %d files", len(entries))
	}
}
