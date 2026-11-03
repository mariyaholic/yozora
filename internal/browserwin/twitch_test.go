//go:build windows

package browserwin

import "testing"

func TestMatchTwitchTitle(t *testing.T) {
	for _, tc := range []struct {
		titles        []string
		title, artist string
		want          int
	}{
		{[]string{"streamer - Twitch - Google Chrome"}, "Stream title", "streamer", 1},
		{[]string{"Streamer - Twitch - Microsoft​ Edge"}, "Stream title", "streamer", 1},
		{[]string{"(2) streamer - Twitch - Microsoft Edge"}, "Stream title", "streamer", 1},
		{[]string{"streamer - Twitch - Personal - Microsoft Edge"}, "Stream title", "streamer", 1},
		{[]string{"streamer - Twitch and 3 more pages - Personal - Microsoft Edge"}, "Stream title", "streamer", 1},
		{[]string{"streamer - Twitch - Google Chrome"}, "streamer - Twitch", "", 1},
		{[]string{"streamer - Twitch - Google Chrome"}, "streamer", "", 1},
		{[]string{"streamer - Twitch", "streamer - Twitch - Google Chrome"}, "Stream title", "streamer", 2},
		{[]string{"other - Twitch - Google Chrome"}, "Stream title", "streamer", 0},
		{[]string{"streamer - YouTube - Google Chrome"}, "Stream title", "streamer", 0},
		{[]string{"Video - Twitch - YouTube - Microsoft Edge"}, "Video - Twitch", "", 0},
		{[]string{" - Twitch"}, "Stream title", "", 0},
		{[]string{"streamer - Twitch"}, "", "streamer", 0},
	} {
		index, count := MatchTwitchTitle(tc.titles, tc.title, tc.artist)
		if count != tc.want || (count == 1) != (index >= 0) {
			t.Errorf("%q %q %q: index=%d count=%d", tc.titles, tc.title, tc.artist, index, count)
		}
	}
}

func TestParseTwitchChannel(t *testing.T) {
	for raw, want := range map[string]string{
		"https://www.twitch.tv/Streamer":      "streamer",
		"twitch.tv/streamer_01":               "streamer_01",
		"https://m.twitch.tv/streamer/":       "streamer",
		"https://www.twitch.tv/streamer?x=1":  "streamer",
		"https://www.twitch.tv/videos/123456": "",
		"https://www.twitch.tv/directory":     "",
		"https://www.twitch.tv/streamer/clip": "",
		"https://www.twitch.tv/":              "",
		"https://evil.test/streamer":          "",
		"https://user@www.twitch.tv/streamer": "",
		"ftp://www.twitch.tv/streamer":        "",
		"https://www.twitch.tv/../x":          "",
		"https://www.twitch.tv/ab":            "",
	} {
		got, ok := ParseTwitchChannel(raw)
		if got != want || ok != (want != "") {
			t.Errorf("%q: %q %v", raw, got, ok)
		}
	}
}
