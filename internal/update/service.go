package update

import (
	"context"
	"log"
	"sync"
	"time"
)

// Install phases shown by the panel.
const (
	PhaseIdle        = "idle"
	PhaseDownloading = "downloading"
	PhaseVerifying   = "verifying"
	PhaseInstalling  = "installing"
	PhaseError       = "error"
)

// installGrace is how long a launched installer is trusted to end this daemon.
const installGrace = 3 * time.Minute

// installTimeout bounds one download, verify and launch.
const installTimeout = 15 * time.Minute

// Service is the update checker and installer for one daemon.
type Service struct {
	version   string
	statePath string
	enabled   func() bool
	// fetch, launch, selfSig and fileSig are replaced in tests.
	fetch   func(ctx context.Context) (string, error)
	launch  func(path string, args []string) error
	selfSig func() Signature
	fileSig func(path string) Signature
	dl      *downloader
	jitter  func() time.Duration

	selfOnce sync.Once
	self     Signature

	mu    sync.Mutex
	st    State
	phase string
	err   string
	since time.Time // when the installer was launched
}

// New builds the service for a running version. enabled reports the
// "[update] check" setting and is read each time a check comes due.
func New(version, statePath string, enabled func() bool) *Service {
	client := newClient(DefaultAllowURL, checkTimeout)
	s := &Service{
		version: version, statePath: statePath, enabled: enabled,
		launch: launch, selfSig: selfSignature, fileSig: FileSignature,
		dl:    newDownloader(downloadBase, DefaultAllowURL, ""),
		phase: PhaseIdle,
	}
	s.fetch = func(ctx context.Context) (string, error) { return fetchLatest(ctx, client, latestURL, version) }
	s.jitter = func() time.Duration { return time.Duration(randInt63n(int64(MaxJitter))) }
	s.st = loadState(statePath)
	return s
}

// Active reports whether this build checks for updates at all: a local "dev"
// build has no version to compare.
func (s *Service) Active() bool {
	_, ok := ParseVersion(s.version)
	return ok
}

// Run checks on schedule until ctx ends. It is one timer, asleep between
// checks: about 2 minutes after start, then once a day (the remembered time
// carries over restarts). It returns at once for a dev build.
func (s *Service) Run(ctx context.Context) {
	if !s.Active() {
		return
	}
	timer := time.NewTimer(NextDelay(s.state(), time.Now(), s.jitter()))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		d := Interval
		if s.enabled() {
			s.CheckNow(ctx)
			d = NextDelay(s.state(), time.Now(), s.jitter())
		}
		timer.Reset(d)
	}
}

func (s *Service) state() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st
}

// CheckNow asks GitHub once and remembers the answer.
func (s *Service) CheckNow(ctx context.Context) error {
	now := time.Now()
	latest, err := s.fetch(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.AttemptAt = now
	if err == nil {
		s.st.CheckedAt = now
		s.st.Latest = latest
	}
	if serr := saveState(s.statePath, s.st); serr != nil {
		log.Printf("update: remember check: %v", serr)
	}
	if err != nil {
		log.Printf("update: check failed: %v", err)
	}
	return err
}

// Available returns the newer version, if the last check found one.
func (s *Service) Available() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.Latest != "" && Newer(s.version, s.st.Latest) {
		return s.st.Latest, true
	}
	return "", false
}

// InstallStatus is the install progress the panel shows.
type InstallStatus struct {
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// Status is the /api/update answer.
type Status struct {
	Version      string        `json:"version"`
	Latest       string        `json:"latest"`
	Available    bool          `json:"available"`
	URL          string        `json:"url"`
	Verification string        `json:"verification"`
	CheckEnabled bool          `json:"check_enabled"`
	Install      InstallStatus `json:"install"`
}

func (s *Service) Status() Status {
	latest, ok := s.Available()
	st := Status{Version: s.version, Available: ok, Verification: Mode(s.selfSignature()), CheckEnabled: s.Active() && s.enabled()}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ok {
		st.Latest = latest
		st.URL = ReleaseURL(latest)
	}
	st.Install = InstallStatus{State: s.phase, Error: s.err}
	// Setup normally ends this daemon within seconds. If it is still here long
	// after the launch, the installer did not go through; allow another try.
	if s.phase == PhaseInstalling && time.Since(s.since) > installGrace {
		s.phase = PhaseIdle
		st.Install.State = PhaseIdle
	}
	return st
}

// selfSignature is the running exe's signature, read once.
func (s *Service) selfSignature() Signature {
	s.selfOnce.Do(func() { s.self = s.selfSig() })
	return s.self
}

func (s *Service) setPhase(phase, msg string) {
	s.mu.Lock()
	s.phase, s.err = phase, msg
	s.mu.Unlock()
}

// begin claims the install slot for the version the last check found.
func (s *Service) begin() (string, error) {
	latest, ok := s.Available()
	if !ok {
		return "", ErrNoUpdate
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase == PhaseInstalling && time.Since(s.since) > installGrace {
		s.phase = PhaseIdle
	}
	if s.phase != PhaseIdle && s.phase != PhaseError {
		return "", ErrBusy
	}
	s.phase, s.err = PhaseDownloading, ""
	return latest, nil
}

// StartInstall begins an install in the background and returns at once; the
// progress is in Status. It runs only on an explicit request from the user.
func (s *Service) StartInstall() error {
	latest, err := s.begin()
	if err != nil {
		return err
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
		defer cancel()
		_ = s.run(ctx, latest)
	}()
	return nil
}

// Install is StartInstall that waits until the installer has been launched.
func (s *Service) Install(ctx context.Context) error {
	latest, err := s.begin()
	if err != nil {
		return err
	}
	return s.run(ctx, latest)
}

// run downloads, verifies and starts the installer. Setup then stops this
// daemon itself, which is what lets it notice that Yozora was running and
// start it again afterwards.
func (s *Service) run(ctx context.Context, latest string) (err error) {
	defer func() {
		if err != nil {
			log.Printf("update: install %s: %v", latest, err)
			s.setPhase(PhaseError, err.Error())
		}
	}()

	dir, installer, sum, sums, err := s.dl.get(ctx, latest, func(p string) { s.setPhase(p, "") })
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			removeAll(dir)
		}
	}()
	s.setPhase(PhaseVerifying, "")
	if err = VerifyChecksum(sums, installerName, sum); err != nil {
		return err
	}
	if err = CheckSignature(s.selfSignature(), s.fileSig(installer)); err != nil {
		return err
	}
	s.setPhase(PhaseInstalling, "")
	s.mu.Lock()
	s.since = time.Now()
	s.mu.Unlock()
	if err = s.launch(installer, InstallerArgs); err != nil {
		return err
	}
	log.Printf("update: installer for %s started", latest)
	return nil
}
