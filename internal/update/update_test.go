package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		cur, latest string
		want        bool
	}{
		{"0.1.9", "v0.1.10", true}, // numeric, not lexical
		{"0.1.10", "v0.1.9", false},
		{"0.1.5", "v0.1.5", false},
		{"0.1.5", "v0.2.0", true},
		{"0.1.5", "v1.0.0", true},
		{"dev", "v0.1.5", false},
		{"0.1.5", "latest", false},
		{"0.1.5", "v0.1", false},
		{"0.1.5", "v0.1.5-rc1", false},
		{"0.1.5", "v0.1.+5", false},
		{"0.1.5", "v0.1.99999999999", false},
	} {
		if got := Newer(c.cur, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.cur, c.latest, got, c.want)
		}
	}
}

func TestNextDelay(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name string
		st   State
		want time.Duration
	}{
		{"never checked", State{}, StartupDelay},
		{"checked an hour ago", State{CheckedAt: now.Add(-time.Hour), AttemptAt: now.Add(-time.Hour)}, 23 * time.Hour},
		{"overdue waits only the startup delay", State{CheckedAt: now.Add(-30 * time.Hour)}, StartupDelay},
		{"failed just now retries in an hour", State{CheckedAt: now.Add(-2 * time.Hour), AttemptAt: now}, time.Hour},
		{"failed with no success yet", State{AttemptAt: now}, time.Hour},
		{"failure does not push past the daily check", State{CheckedAt: now.Add(-23*time.Hour - 30*time.Minute), AttemptAt: now}, 30 * time.Minute},
		{"clock set back is capped", State{CheckedAt: now.Add(100 * 24 * time.Hour)}, Interval},
	} {
		if got := NextDelay(c.st, now, 0); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if got := NextDelay(State{}, now, 7*time.Second); got != StartupDelay+7*time.Second {
		t.Errorf("jitter not added: %v", got)
	}
}

func newTestService(t *testing.T, version string, enabled bool) (*Service, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	s := New(version, filepath.Join(t.TempDir(), "update.json"), func() bool { return enabled })
	s.jitter = func() time.Duration { return 0 }
	s.fetch = func(context.Context) (string, error) { calls.Add(1); return "0.1.9", nil }
	return s, &calls
}

func TestRunChecksOnceAfterStartupThenDaily(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, calls := newTestService(t, "0.1.2", true)
		ctx, cancel := context.WithCancel(context.Background())
		go s.Run(ctx)
		time.Sleep(StartupDelay - time.Second)
		synctest.Wait()
		if calls.Load() != 0 {
			t.Fatal("checked before the startup delay")
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatalf("checks after startup delay = %d, want 1", calls.Load())
		}
		if v, ok := s.Available(); !ok || v != "0.1.9" {
			t.Fatalf("Available = %q, %v", v, ok)
		}
		time.Sleep(Interval - time.Hour)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatal("checked again within 24 hours")
		}
		time.Sleep(time.Hour + time.Second)
		synctest.Wait()
		if calls.Load() != 2 {
			t.Fatalf("checks after a day = %d, want 2", calls.Load())
		}
		cancel()
		synctest.Wait()
	})
}

func TestRunSkipsWhenDisabledAndForDevBuilds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, calls := newTestService(t, "0.1.2", false)
		ctx, cancel := context.WithCancel(context.Background())
		go s.Run(ctx)
		time.Sleep(3 * Interval)
		synctest.Wait()
		if calls.Load() != 0 {
			t.Fatalf("a disabled check ran %d times", calls.Load())
		}
		cancel()
		synctest.Wait()

		dev, devCalls := newTestService(t, "dev", true)
		done := make(chan struct{})
		go func() { dev.Run(context.Background()); close(done) }()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("Run kept waiting on a dev build")
		}
		if devCalls.Load() != 0 {
			t.Fatal("a dev build checked")
		}
	})
}

func TestRestartDoesNotRecheck(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, calls := newTestService(t, "0.1.2", true)
		if err := s.CheckNow(context.Background()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Hour)
		again := New("0.1.2", s.statePath, func() bool { return true })
		again.jitter = func() time.Duration { return 0 }
		again.fetch = s.fetch
		if v, ok := again.Available(); !ok || v != "0.1.9" {
			t.Fatalf("remembered result lost: %q %v", v, ok)
		}
		ctx, cancel := context.WithCancel(context.Background())
		go again.Run(ctx)
		time.Sleep(10 * time.Hour)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatalf("a restart re-checked: %d calls", calls.Load())
		}
		cancel()
		synctest.Wait()
	})
}

func TestFailedCheckRetriesSoonerAndKeepsLastResult(t *testing.T) {
	s, _ := newTestService(t, "0.1.2", true)
	if err := s.CheckNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.fetch = func(context.Context) (string, error) { return "", http.ErrHandlerTimeout }
	if err := s.CheckNow(context.Background()); err == nil {
		t.Fatal("expected failure")
	}
	st := loadState(s.statePath)
	if st.Latest != "0.1.9" || !st.AttemptAt.After(st.CheckedAt) {
		t.Fatalf("state after failure = %+v", st)
	}
	if got := NextDelay(st, st.AttemptAt, 0); got != time.Hour {
		t.Fatalf("retry delay = %v", got)
	}
}

func TestStateIgnoresGarbage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "update.json")
	os.WriteFile(p, []byte(`{"latest":"not a version","checked_at":"nope"`), 0o600)
	if st := loadState(p); st.Latest != "" || !st.CheckedAt.IsZero() {
		t.Fatalf("garbage state = %+v", st)
	}
	if st := loadState(filepath.Join(t.TempDir(), "missing.json")); st != (State{}) {
		t.Fatal("missing state not empty")
	}
}

func TestFetchLatest(t *testing.T) {
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.UserAgent()
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte(`{"tag_name":"v0.1.12","html_url":"https://evil.example/x"}`))
		case "/badtag":
			w.Write([]byte(`{"tag_name":"nightly"}`))
		case "/junk":
			w.Write([]byte(`<html>`))
		default:
			http.Error(w, "rate limited", 403)
		}
	}))
	defer srv.Close()
	c := srv.Client()
	if v, err := fetchLatest(context.Background(), c, srv.URL+"/ok", "0.1.3"); err != nil || v != "0.1.12" {
		t.Fatalf("ok: %q %v", v, err)
	}
	if ua != "Yozora/0.1.3" {
		t.Fatalf("User-Agent = %q", ua)
	}
	for _, p := range []string{"/badtag", "/junk", "/limited"} {
		if _, err := fetchLatest(context.Background(), c, srv.URL+p, "0.1.3"); err == nil {
			t.Errorf("%s accepted", p)
		}
	}
	if got := ReleaseURL("0.1.12"); got != "https://github.com/mariyaholic/yozora/releases/tag/v0.1.12" {
		t.Fatalf("ReleaseURL = %s", got)
	}
}

func TestDefaultAllowURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://github.com/mariyaholic/yozora/releases/download/v0.1.1/x": true,
		"https://objects.githubusercontent.com/abc":                        true,
		"https://release-assets.githubusercontent.com/abc":                 true,
		"http://github.com/x":                                              false,
		"https://github.com.evil.example/x":                                false,
		"https://evilgithub.com/x":                                         false,
		"https://github.com:8443/x":                                        false,
		"https://user@github.com/x":                                        false,
		"https://githubusercontent.com/x":                                  false,
		"file:///c:/x":                                                     false,
	} {
		u, _ := url.Parse(raw)
		if got := DefaultAllowURL(u); got != want {
			t.Errorf("DefaultAllowURL(%s) = %v, want %v", raw, got, want)
		}
	}
}

func allowHost(host string) func(*url.URL) bool {
	return func(u *url.URL) bool { return u.Host == host }
}

func TestDownloadRejectsOffHostRedirect(t *testing.T) {
	var hit atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit.Store(true); w.Write([]byte("x")) }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/payload", http.StatusFound)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	d := newDownloader(srv.URL, allowHost(u.Host), t.TempDir())
	if _, err := d.fetch(context.Background(), "0.1.2", installerName, filepath.Join(t.TempDir(), "x"), 1<<20); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("off-host redirect: %v", err)
	}
	if hit.Load() {
		t.Fatal("the redirect target was contacted")
	}
}

func TestDownloadRejectsOversize(t *testing.T) {
	big := strings.Repeat("a", 2000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0.1.2/chunked" {
			// No Content-Length: the cap has to hold on the bytes actually read.
			w.(http.Flusher).Flush()
		}
		w.Write([]byte(big))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	d := newDownloader(srv.URL, allowHost(u.Host), t.TempDir())
	for _, name := range []string{"sized", "chunked"} {
		dst := filepath.Join(t.TempDir(), "x")
		if _, err := d.fetch(context.Background(), "0.1.2", name, dst, 1000); err == nil || !strings.Contains(err.Error(), "limit") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := d.fetch(context.Background(), "0.1.2", "fine", filepath.Join(t.TempDir(), "x"), 2000); err != nil {
		t.Fatalf("exactly at the cap: %v", err)
	}
}

type release struct {
	srv    *httptest.Server
	setup  string
	sums   string
	tmp    string
	launch []string
}

func serveRelease(t *testing.T, setup, sums string) *release {
	t.Helper()
	r := &release{setup: setup, sums: sums, tmp: t.TempDir()}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/v0.1.9/Yozora-Setup.exe":
			w.Write([]byte(r.setup))
		case "/v0.1.9/SHA256SUMS.txt":
			w.Write([]byte(r.sums))
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func sumLine(content, name string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:]) + "  " + name + "\n"
}

func installService(t *testing.T, r *release) (*Service, *[][]string) {
	t.Helper()
	s, _ := newTestService(t, "0.1.2", true)
	if err := s.CheckNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(r.srv.URL)
	s.dl = newDownloader(r.srv.URL, allowHost(u.Host), r.tmp)
	var launched [][]string
	s.launch = func(path string, args []string) error {
		launched = append(launched, append([]string{path}, args...))
		return nil
	}
	s.selfSig = func() Signature { return Signature{} }
	s.fileSig = func(string) Signature { return Signature{} }
	return s, &launched
}

func TestInstallVerifiesChecksumThenLaunches(t *testing.T) {
	r := serveRelease(t, "installer bytes", "")
	r.sums = sumLine(r.setup, installerName) + sumLine("other", "Yozora.exe")
	s, launched := installService(t, r)
	if err := s.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*launched) != 1 || filepath.Base((*launched)[0][0]) != installerName {
		t.Fatalf("launched = %v", *launched)
	}
	if got := strings.Join((*launched)[0][1:], " "); !strings.Contains(got, "/VERYSILENT") || !strings.Contains(got, "/SUPPRESSMSGBOXES") || !strings.Contains(got, "/NORESTART") {
		t.Fatalf("installer flags = %q", got)
	}
	st := s.Status()
	if st.Install.State != PhaseInstalling || st.Verification != ModeChecksum || !st.Available || st.Latest != "0.1.9" {
		t.Fatalf("status = %+v", st)
	}
	if err := s.Install(context.Background()); err != ErrBusy {
		t.Fatalf("second install while installing: %v", err)
	}
}

func TestInstallRefusesChecksumMismatchAndMissingEntry(t *testing.T) {
	for name, sums := range map[string]string{
		"mismatch": sumLine("different bytes", installerName),
		"missing":  sumLine("installer bytes", "Yozora.exe"),
		"empty":    "",
	} {
		r := serveRelease(t, "installer bytes", sums)
		s, launched := installService(t, r)
		if err := s.Install(context.Background()); err == nil {
			t.Fatalf("%s: install went ahead", name)
		}
		if len(*launched) != 0 {
			t.Fatalf("%s: installer was launched", name)
		}
		if st := s.Status(); st.Install.State != PhaseError || st.Install.Error == "" {
			t.Fatalf("%s: status = %+v", name, st.Install)
		}
		if left, _ := os.ReadDir(r.tmp); len(left) != 0 {
			t.Fatalf("%s: download folder left behind", name)
		}
	}
}

func TestInstallRequiresMatchingSignatureWhenRunningExeIsSigned(t *testing.T) {
	r := serveRelease(t, "installer bytes", "")
	r.sums = sumLine(r.setup, installerName)
	for _, c := range []struct {
		name      string
		installer Signature
		ok        bool
	}{
		{"same subject", Signature{true, "CN=SignPath Foundation"}, true},
		{"same subject, other case", Signature{true, "cn=signpath foundation"}, true},
		{"other subject", Signature{true, "CN=Someone Else"}, false},
		{"unsigned installer", Signature{}, false},
		{"invalid installer", Signature{false, "CN=SignPath Foundation"}, false},
	} {
		s, launched := installService(t, r)
		s.selfSig = func() Signature { return Signature{true, "CN=SignPath Foundation"} }
		s.fileSig = func(string) Signature { return c.installer }
		err := s.Install(context.Background())
		if (err == nil) != c.ok || (len(*launched) == 1) != c.ok {
			t.Errorf("%s: err=%v launched=%d", c.name, err, len(*launched))
		}
		if v := s.Status().Verification; v != ModeSignature {
			t.Errorf("%s: verification = %s", c.name, v)
		}
	}
}

func TestVerificationModeDecision(t *testing.T) {
	for _, c := range []struct {
		self Signature
		want string
	}{
		{Signature{}, ModeChecksum},
		{Signature{Valid: false, Subject: "CN=X"}, ModeChecksum},
		{Signature{Valid: true}, ModeChecksum}, // valid but unnamed cannot be compared
		{Signature{Valid: true, Subject: "CN=X"}, ModeSignature},
	} {
		if got := Mode(c.self); got != c.want {
			t.Errorf("Mode(%+v) = %s, want %s", c.self, got, c.want)
		}
	}
	if CheckSignature(Signature{}, Signature{}) != nil {
		t.Error("checksum mode must not require a signature")
	}
}

func TestInstallNeedsAnAvailableUpdate(t *testing.T) {
	s, _ := newTestService(t, "0.1.9", true)
	s.CheckNow(context.Background()) // finds 0.1.9: not newer
	if err := s.Install(context.Background()); err == nil {
		t.Fatal("installed with no update available")
	}
	if st := s.Status(); st.Available || st.Latest != "" || st.URL != "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestParseSums(t *testing.T) {
	h := strings.Repeat("ab", 32)
	got := ParseSums([]byte("\xef\xbb\xbf" + strings.ToUpper(h) + " *Yozora.exe\r\n" + h + "  Yozora-Setup.exe\r\nshort  x\n\nnot-hex" + strings.Repeat("z", 57) + "  y\n"))
	if len(got) != 2 || got["Yozora.exe"] != h || got["Yozora-Setup.exe"] != h {
		t.Fatalf("ParseSums = %v", got)
	}
}

func TestStartInstallReportsProgressInStatus(t *testing.T) {
	r := serveRelease(t, "installer bytes", "")
	r.sums = sumLine(r.setup, installerName)
	s, _ := installService(t, r)
	done := make(chan struct{})
	s.launch = func(string, []string) error { close(done); return nil }
	if err := s.StartInstall(); err != nil {
		t.Fatal(err)
	}
	if st := s.Status().Install.State; st == PhaseIdle {
		t.Fatal("status still idle right after the request")
	}
	if err := s.StartInstall(); err != ErrBusy {
		t.Fatalf("second request: %v", err)
	}
	<-done
}
