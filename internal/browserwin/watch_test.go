//go:build windows

package browserwin

import "testing"

func TestParseYouTubeWatchID(t *testing.T) {
	for _, raw := range []string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=1s&list=PL123",
		"www.youtube.com/watch?v=dQw4w9WgXcQ",
		"youtube.com/watch?v=dQw4w9WgXcQ",
		"m.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ",
		"http://youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtube.com/watch?v=dQw4w9WgXcQ&v=dQw4w9WgXcQ",
	} {
		if id, ok := ParseYouTubeWatchID(raw); !ok || id != "dQw4w9WgXcQ" {
			t.Errorf("valid case rejected: %q", raw)
		}
	}
	for _, raw := range []string{
		"https://youtube.com.evil.com/watch?v=dQw4w9WgXcQ",
		"https://evilyoutube.com/watch?v=dQw4w9WgXcQ",
		"user@www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://user@www.youtube.com/watch?v=dQw4w9WgXcQ",
		"www.youtube.com:443/watch?v=dQw4w9WgXcQ",
		"https://www.youtube.com:/watch?v=dQw4w9WgXcQ",
		"https://youtube.com/watch?v=1234567890",
		"https://youtube.com/watch?v=123456789012",
		"https://youtube.com/watch?v=1234567890!",
		"https://youtube.com/watch?v=dQw4w9WgXcQ&v=Abc_1234-xy",
		"https://youtube.com/watch?v=dQw4w9WgXcQ&v=",
		"https://youtube.com/shorts/dQw4w9WgXcQ",
		"https://youtube.com/watch", "", "javascript:alert(1)",
		"ftp://youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ",
	} {
		if id, ok := ParseYouTubeWatchID(raw); ok || id != "" {
			t.Errorf("invalid case accepted: %q", raw)
		}
	}
}
