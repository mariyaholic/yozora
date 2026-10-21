//go:build windows

package browserwin

import "testing"

func TestMatchYouTubeTabName(t *testing.T) {
	const exact = "Video - YouTube - Audio playing"
	for _, tc := range []struct {
		name  string
		names []string
		title string
		want  bool
	}{
		{"notification", []string{"(3) " + exact}, "Video", true},
		{"exact", []string{exact}, "Video", true},
		{"memory", []string{exact + " - Memory usage - 317 MB"}, "Video", true},
		{"discarded", []string{"Video - YouTube"}, "Video", false},
		{"duplicates", []string{exact, exact + " - Memory usage - 317 MB"}, "Video", true},
		{"music", []string{"Video - YouTube Music - Audio playing"}, "Video", false},
		{"prefix", []string{exact + "ish"}, "Video", false},
		{"title prefix", []string{"Other " + exact}, "Video", false},
		{"empty", []string{" - YouTube - Audio playing"}, "", false},
		{"unrelated", []string{"Other", exact}, "Video", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := MatchYouTubeTabName(tc.names, tc.title); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
