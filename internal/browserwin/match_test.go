//go:build windows

package browserwin

import "testing"

func TestStripBrowserDecoration(t *testing.T) {
	for _, title := range []string{
		"Video - YouTube", "Video - YouTube - Google Chrome",
		"Video - YouTube - Microsoft\u200b Edge", "Video - YouTube - Microsoft Edge",
		"Video - YouTube and 10 more pages - Microsoft Edge",
		"Video - YouTube and 1 more page - Microsoft Edge",
		"Video - YouTube and 1 more page - Microsoft\u200b Edge",
		"Video - YouTube and 1 more page - Personal - Microsoft Edge",
		"Video - YouTube and 1 more page - Personal - Microsoft\u200b Edge",
		"Video - YouTube - Personal - Microsoft\u200b Edge",
		"Video - YouTube and 10 more pages - Personal - Microsoft Edge",
		"(3) Video - YouTube - Google Chrome", "(999+) Video - YouTube",
	} {
		if got := StripBrowserDecoration(title); got != "Video - YouTube" {
			t.Errorf("%q: %q", title, got)
		}
	}
}

func TestMatchYouTubeTitle(t *testing.T) {
	for _, tc := range []struct {
		titles []string
		want   int
	}{
		{[]string{"Video - YouTube - Google Chrome"}, 1},
		{[]string{"(3) Video - YouTube - Microsoft Edge"}, 1},
		{[]string{"Video - YouTube", "Video - YouTube - Google Chrome"}, 2},
		{[]string{"Video - YouTube Music - Google Chrome"}, 0},
		{[]string{"Video - YouTube extra - Google Chrome"}, 0},
		{[]string{"Video - YouTube - trailer - Google Chrome"}, 0},
		{[]string{"Other Video - YouTube"}, 0},
		{[]string{"(1000) Video - YouTube"}, 0},
		{nil, 0},
	} {
		i, count := MatchYouTubeTitle(tc.titles, "Video")
		if count != tc.want || (count == 1 && i != 0) || (count != 1 && i != -1) {
			t.Errorf("%v: %d %v", tc.titles, i, count)
		}
	}
}
