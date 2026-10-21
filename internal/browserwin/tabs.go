//go:build windows

package browserwin

import "strings"

// MatchYouTubeTabName reports whether at least one audio-playing YouTube tab matches.
// Ambiguous matches still classify as YouTube so blocked tracks cannot leak.
func MatchYouTubeTabName(names []string, smtcTitle string) bool {
	if smtcTitle == "" {
		return false
	}
	target := smtcTitle + " - YouTube - Audio playing"
	for _, name := range names {
		name = notificationPrefix.ReplaceAllString(name, "")
		if name == target || strings.HasPrefix(name, target+" - ") {
			return true
		}
	}
	return false
}
