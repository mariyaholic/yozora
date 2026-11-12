package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	installerName = "Yozora-Setup.exe"
	sumsName      = "SHA256SUMS.txt"
	// maxInstaller is well above the real installer (a few MB) and well below anything that fills a disk.
	maxInstaller  = 50 << 20
	maxSums       = 64 << 10
	tempPrefix    = "yozora-update-"
	staleAfter    = 24 * time.Hour
	downloadLimit = 10 * time.Minute

	downloadBase = "https://github.com/" + Repo + "/releases/download"
)

// InstallerArgs run Setup with no questions and no restart. The installer
// stops the running Yozora itself and starts it again afterwards.
var InstallerArgs = []string{"/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/NORESTARTAPPLICATIONS"}

// downloader fetches the two release assets over HTTPS from allowed hosts.
type downloader struct {
	base   string
	client *http.Client
	allow  func(*url.URL) bool
	tmp    string // parent of the fresh directory; "" means the system temp dir
}

func newDownloader(base string, allow func(*url.URL) bool, tmp string) *downloader {
	return &downloader{base: base, allow: allow, tmp: tmp, client: newClient(allow, downloadLimit)}
}

// fetch downloads one asset of tag into dst, refusing more than max bytes,
// and returns its SHA-256.
func (d *downloader) fetch(ctx context.Context, tag, name, dst string, max int64) (string, error) {
	u, err := url.Parse(d.base + "/v" + tag + "/" + name)
	if err != nil || !d.allow(u) {
		return "", fmt.Errorf("refusing to download %s from %s", name, d.base)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Yozora")
	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: github answered %s", name, resp.Status)
	}
	if resp.ContentLength > max {
		return "", fmt.Errorf("download %s: larger than the %d MB limit", name, max>>20)
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, max+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("download %s: %w", name, err)
	}
	if n > max {
		return "", fmt.Errorf("download %s: larger than the %d MB limit", name, max>>20)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// get downloads the installer and the checksum list into a fresh directory
// and returns the installer's path and computed SHA-256 along with the list.
// The directory is removed again on any error.
func (d *downloader) get(ctx context.Context, tag string, stage func(string)) (dir, installer, sum string, sums map[string]string, err error) {
	sweepStale(d.tmp)
	dir, err = os.MkdirTemp(d.tmp, tempPrefix)
	if err != nil {
		return "", "", "", nil, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()
	stage("downloading")
	installer = filepath.Join(dir, installerName)
	if sum, err = d.fetch(ctx, tag, installerName, installer, maxInstaller); err != nil {
		return "", "", "", nil, err
	}
	sumsPath := filepath.Join(dir, sumsName)
	if _, err = d.fetch(ctx, tag, sumsName, sumsPath, maxSums); err != nil {
		return "", "", "", nil, err
	}
	b, err := os.ReadFile(sumsPath)
	if err != nil {
		return "", "", "", nil, err
	}
	return dir, installer, sum, ParseSums(b), nil
}

// sweepStale removes download folders an earlier run left behind (a running
// installer cannot be deleted by the run that started it).
func sweepStale(parent string) {
	if parent == "" {
		parent = os.TempDir()
	}
	entries, _ := os.ReadDir(parent)
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), tempPrefix) {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > staleAfter {
			os.RemoveAll(filepath.Join(parent, e.Name()))
		}
	}
}

// launch starts the installer detached from this process, so it outlives the
// daemon that Setup is about to stop.
func launch(path string, args []string) error {
	cmd := exec.Command(path, args...)
	cmd.Dir = filepath.Dir(path)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// ErrBusy and ErrNoUpdate are the refusals a request to install can get.
var (
	ErrBusy     = errors.New("an update is already in progress")
	ErrNoUpdate = errors.New("no update is available")
)
