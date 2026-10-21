//go:build windows

package browserwin

import (
	"regexp"
	"strings"
)

var notificationPrefix = regexp.MustCompile(`^\(\d{1,3}\+?\) `)
var profileSuffix = regexp.MustCompile(`( - YouTube(?: and \d+ more pages?)?) - .+$`)
var morePages = regexp.MustCompile(` and \d+ more pages?$`)

// StripBrowserDecoration removes Chromium's browser, profile and page-count suffixes.
func StripBrowserDecoration(title string) string {
	for _, suffix := range []string{" - Microsoft\u200b Edge", " - Microsoft Edge", " - Google Chrome"} {
		if strings.HasSuffix(title, suffix) {
			title = strings.TrimSuffix(title, suffix)
			// Edge may append a profile after the tab's YouTube suffix.
			if suffix != " - Google Chrome" {
				title = profileSuffix.ReplaceAllString(title, "${1}")
			}
			break
		}
	}
	title = morePages.ReplaceAllString(title, "")
	return notificationPrefix.ReplaceAllString(title, "")
}

// MatchYouTubeTitle counts exact matches; index is valid only when count is one.
func MatchYouTubeTitle(titles []string, smtcTitle string) (index int, count int) {
	index = -1
	if smtcTitle == "" {
		return
	}
	for i, title := range titles {
		if StripBrowserDecoration(title) == smtcTitle+" - YouTube" {
			index = i
			count++
		}
	}
	if count != 1 {
		index = -1
	}
	return
}
