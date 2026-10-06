//go:build windows

package cli

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"uika-resonance/internal/config"
)

type dashboardEndpoint struct {
	BaseURL string `json:"base_url"`
	Token   string `json:"token"`
	PID     int    `json:"pid"`
}

func dashboardURL(endpoint dashboardEndpoint) (*url.URL, error) {
	u, err := url.Parse(endpoint.BaseURL)
	if err != nil {
		return nil, errors.New("invalid settings address")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65535 || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || endpoint.PID <= 0 {
		return nil, errors.New("settings address must be a local Yozora endpoint")
	}
	token, err := hex.DecodeString(endpoint.Token)
	if err != nil || len(token) != 16 {
		return nil, errors.New("invalid settings session")
	}
	u.RawQuery = url.Values{"t": []string{endpoint.Token}}.Encode()
	return u, nil
}

func writeDashboardEndpoint(path string, endpoint dashboardEndpoint) error {
	if _, err := dashboardURL(endpoint); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".dashboard-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := json.NewEncoder(f).Encode(endpoint); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func readDashboardEndpoint(path string) (dashboardEndpoint, error) {
	var endpoint dashboardEndpoint
	f, err := os.Open(path)
	if err != nil {
		return endpoint, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return endpoint, err
	}
	if len(data) > 4096 {
		return endpoint, errors.New("settings endpoint file exceeds size limit")
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&endpoint); err != nil {
		return endpoint, errors.New("invalid settings endpoint file")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return endpoint, errors.New("invalid trailing settings endpoint data")
	}
	_, err = dashboardURL(endpoint)
	return endpoint, err
}

func ensureDashboardConfig(path string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	if cfg.Server.Enabled {
		return nil
	}
	cfg.Server.Enabled = true
	return config.Save(cfg, path)
}

func dashboardEndpointPath(dataDir string) string {
	return filepath.Join(dataDir, "dashboard.json")
}

func waitDashboard(ctx context.Context, path string) (dashboardEndpoint, error) {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return dashboardEndpoint{}, err
		}
		endpoint, err := readDashboardEndpoint(path)
		if err == nil && probeDashboard(ctx, endpoint) {
			return endpoint, nil
		}
		select {
		case <-ctx.Done():
			return dashboardEndpoint{}, ctx.Err()
		case <-tick.C:
		}
	}
}

func probeDashboard(ctx context.Context, endpoint dashboardEndpoint) bool {
	u, err := dashboardURL(endpoint)
	if err != nil {
		return false
	}
	u.Path, u.RawQuery = "/api/sources", ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false
	}
	req.Header.Set("X-Uika-Token", endpoint.Token)
	client := &http.Client{
		Timeout:       750 * time.Millisecond,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var result struct {
		Name    string          `json:"name"`
		Enabled map[string]bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&result); err != nil || result.Name != "Yozora" {
		return false
	}
	for _, source := range []string{"applemusic", "spotify", "spotifyapi", "browser", "generic"} {
		if _, ok := result.Enabled[source]; !ok {
			return false
		}
	}
	return true
}
