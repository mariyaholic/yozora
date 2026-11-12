package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	latestURL    = "https://api.github.com/repos/" + Repo + "/releases/latest"
	checkTimeout = 15 * time.Second
	maxAPIBody   = 1 << 20
)

// allowedHosts are the only hosts a release request or its redirects may reach.
// GitHub serves release assets from its own CDN hosts; release-assets is the
// one it redirects to today, objects the one it used before.
var allowedHosts = map[string]bool{
	"api.github.com":                       true,
	"github.com":                           true,
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
}

// DefaultAllowURL accepts https URLs on GitHub's own hosts and nothing else.
func DefaultAllowURL(u *url.URL) bool {
	return u.Scheme == "https" && u.Port() == "" && u.User == nil && allowedHosts[strings.ToLower(u.Hostname())]
}

// newClient returns a client that refuses redirects off the allowed hosts.
func newClient(allow func(*url.URL) bool, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if !allow(req.URL) {
				return fmt.Errorf("redirect to %s refused", req.URL.Host)
			}
			return nil
		},
	}
}

// fetchLatest asks GitHub for the latest release and returns its version as
// "0.1.N". The request carries only the version in its User-Agent.
func fetchLatest(ctx context.Context, client *http.Client, apiURL, version string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Yozora/"+version)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github answered %s", resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAPIBody)).Decode(&rel); err != nil {
		return "", fmt.Errorf("release answer: %w", err)
	}
	if _, ok := ParseVersion(rel.TagName); !ok {
		return "", fmt.Errorf("release tag %q is not a version", rel.TagName)
	}
	return strings.TrimPrefix(rel.TagName, "v"), nil
}
