//go:build windows

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"uika-resonance/internal/config"
	"uika-resonance/internal/update"
)

type fakeUpdater struct {
	st       update.Status
	err      error
	installs int
}

func (f *fakeUpdater) Status() update.Status { return f.st }
func (f *fakeUpdater) StartInstall() error {
	if f.err != nil {
		return f.err
	}
	f.installs++
	f.st.Install = update.InstallStatus{State: update.PhaseDownloading}
	return nil
}

func updateHandler(t *testing.T, u Updater) http.Handler {
	t.Helper()
	path := filepath.Join(t.TempDir(), "c.toml")
	if err := config.Save(config.Defaults(), path); err != nil {
		t.Fatal(err)
	}
	h, err := config.NewHolder(path)
	if err != nil {
		t.Fatal(err)
	}
	return Handler(Deps{Cfg: h, CfgPath: path, Token: "fixture-token", Update: u})
}

func TestUpdateAPIStates(t *testing.T) {
	avail := update.Status{Version: "0.1.2", Latest: "0.1.9", Available: true, URL: update.ReleaseURL("0.1.9"), Verification: update.ModeChecksum, CheckEnabled: true,
		Install: update.InstallStatus{State: update.PhaseIdle}}
	for name, c := range map[string]struct {
		u    Updater
		want update.Status
	}{
		"no service":  {nil, update.Status{Verification: update.ModeChecksum, Install: update.InstallStatus{State: update.PhaseIdle}}},
		"available":   {&fakeUpdater{st: avail}, avail},
		"signed":      {&fakeUpdater{st: func() update.Status { s := avail; s.Verification = update.ModeSignature; return s }()}, func() update.Status { s := avail; s.Verification = update.ModeSignature; return s }()},
		"downloading": {&fakeUpdater{st: func() update.Status { s := avail; s.Install.State = update.PhaseDownloading; return s }()}, func() update.Status { s := avail; s.Install.State = update.PhaseDownloading; return s }()},
		"failed": {&fakeUpdater{st: func() update.Status {
			s := avail
			s.Install = update.InstallStatus{State: update.PhaseError, Error: "checksum mismatch"}
			return s
		}()}, func() update.Status {
			s := avail
			s.Install = update.InstallStatus{State: update.PhaseError, Error: "checksum mismatch"}
			return s
		}()},
	} {
		w := request(updateHandler(t, c.u), "GET", "/api/update", "", true)
		if w.Code != 200 {
			t.Fatalf("%s: %d", name, w.Code)
		}
		var got update.Status
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got != c.want {
			t.Errorf("%s: %+v (%v), want %+v", name, got, err, c.want)
		}
	}
	var raw map[string]any
	w := request(updateHandler(t, &fakeUpdater{st: avail}), "GET", "/api/update", "", true)
	_ = json.Unmarshal(w.Body.Bytes(), &raw)
	for _, key := range []string{"version", "latest", "available", "url", "verification"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("/api/update lacks %q", key)
		}
	}
}

func TestUpdateAPIRequiresTokenAndMethods(t *testing.T) {
	f := &fakeUpdater{st: update.Status{Available: true}}
	h := updateHandler(t, f)
	for _, p := range []string{"/api/update", "/api/update/install"} {
		if w := request(h, "POST", p, "", false); w.Code != 401 {
			t.Errorf("%s without a token: %d", p, w.Code)
		}
	}
	if w := request(h, "GET", "/api/update/install", "", true); w.Code != 405 {
		t.Errorf("GET install: %d", w.Code)
	}
	if w := request(h, "POST", "/api/update", "", true); w.Code != 405 {
		t.Errorf("POST update: %d", w.Code)
	}
	if f.installs != 0 {
		t.Fatal("an unauthenticated or wrong-method request started an install")
	}
}

func TestUpdateInstallIsExplicitAndRefusesWhenNotPossible(t *testing.T) {
	f := &fakeUpdater{st: update.Status{Available: true, Install: update.InstallStatus{State: update.PhaseIdle}}}
	h := updateHandler(t, f)
	if request(h, "GET", "/api/update", "", true); f.installs != 0 {
		t.Fatal("reading the status started an install")
	}
	w := request(h, "POST", "/api/update/install", "", true)
	if w.Code != http.StatusAccepted || f.installs != 1 {
		t.Fatalf("install: %d, installs %d", w.Code, f.installs)
	}
	var st update.Status
	_ = json.Unmarshal(w.Body.Bytes(), &st)
	if st.Install.State != update.PhaseDownloading {
		t.Fatalf("install answer = %+v", st)
	}
	f.err = update.ErrBusy
	if w := request(h, "POST", "/api/update/install", "", true); w.Code != http.StatusConflict {
		t.Fatalf("busy install: %d", w.Code)
	}
	f.err = errors.New("no update is available")
	if w := request(h, "POST", "/api/update/install", "", true); w.Code != http.StatusConflict {
		t.Fatalf("no update: %d", w.Code)
	}
	if w := request(updateHandler(t, nil), "POST", "/api/update/install", "", true); w.Code != http.StatusConflict {
		t.Fatalf("no service: %d", w.Code)
	}
}
