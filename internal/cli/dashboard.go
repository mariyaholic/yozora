//go:build windows

package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"uika-resonance/internal/config"
	"uika-resonance/internal/sysutil"
)

const dashboardLaunchTimeout = 25 * time.Second

const launcherSingletonName = `Local\uika-resonance-control-panel`

// trayPanelWait bounds how long a tray-launched panel waits for the daemon's
// endpoint. The tray only exists once the endpoint is written, so a miss means
// the daemon is shutting down.
const trayPanelWait = 3 * time.Second

// panelHandoffWait covers a second launch that lands while the previous panel
// is closing: its window is gone but its process still holds the mutex.
const panelHandoffWait = 2 * time.Second

// Dashboard is the double-click entry point: it opens the control panel,
// starting the serve daemon first when none is running.
func Dashboard() int { return runDashboard(true) }

// trayPanel is the hidden `panel` command the daemon's tray runs. It never
// starts a daemon, so a panel launched just before Quit cannot revive one.
func trayPanel() int { return runDashboard(false) }

func runDashboard(startDaemon bool) int {
	hideLauncherConsole()
	owner, release, err := claimPanel(
		func() (bool, func(), error) { return sysutil.AcquireNamedMutex(launcherSingletonName) },
		focusRunningPanel, panelHandoffWait, 100*time.Millisecond)
	if err != nil {
		launcherNotify("Yozora", "Could not start: "+err.Error(), notifyError)
		return 1
	}
	if !owner {
		return 0
	}
	defer release()
	cfgPath, dataDir, _ := config.Paths()
	if err := ensureDashboardConfig(cfgPath); err != nil {
		launcherNotify("Yozora", "Could not prepare the settings file:\n"+err.Error(), notifyError)
		return 1
	}
	endpointPath := dashboardEndpointPath(dataDir)
	if !startDaemon {
		ctx, cancel := context.WithTimeout(context.Background(), trayPanelWait)
		defer cancel()
		endpoint, err := waitDashboard(ctx, endpointPath)
		if err != nil {
			launcherLog(dataDir, "tray panel: daemon endpoint unavailable; not opening")
			return 1
		}
		return presentDashboard(endpoint, dataDir)
	}
	ctx, cancel := context.WithTimeout(context.Background(), dashboardLaunchTimeout)
	defer cancel()

	if endpoint, err := readDashboardEndpoint(endpointPath); err == nil && probeDashboard(ctx, endpoint) {
		return presentDashboard(endpoint, dataDir)
	}

	exe, err := daemonExecutable()
	if err != nil {
		launcherNotify("Yozora", err.Error(), notifyError)
		return 1
	}
	if err := spawnDaemon(exe); err != nil {
		launcherNotify("Yozora", "Could not start Yozora:\n"+err.Error(), notifyError)
		return 1
	}
	endpoint, err := waitDashboard(ctx, endpointPath)
	if err != nil {
		launcherNotify("Yozora", "Yozora did not open its control panel in time.\nDiagnose with:  Yozora.exe doctor", notifyError)
		return 1
	}
	return presentDashboard(endpoint, dataDir)
}

// claimPanel makes this process the panel owner, or focuses the panel that
// already is. While an owner exists but shows no window (it is closing), it
// retries until the owner exits or the wait runs out, so the launch is not
// lost.
func claimPanel(acquire func() (bool, func(), error), focus func() bool, wait, step time.Duration) (bool, func(), error) {
	deadline := time.Now().Add(wait)
	for {
		owner, release, err := acquire()
		if err != nil || owner {
			return owner, release, err
		}
		if focus() || !time.Now().Before(deadline) {
			return false, func() {}, nil
		}
		time.Sleep(step)
	}
}

func presentDashboard(endpoint dashboardEndpoint, dataDir string) int {
	if openDashboardWindow(endpoint, dataDir) {
		return 0
	}
	launcherNotify("Yozora", "WebView2 is unavailable, so the control panel is opening in your default browser instead.", notifyInfo)
	return openDashboard(endpoint)
}

func openDashboard(endpoint dashboardEndpoint) int {
	u, err := dashboardURL(endpoint)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	openBrowser(u.String())
	fmt.Println("Yozora control panel opened in your browser.")
	return 0
}

func daemonExecutable() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(self); err != nil || st.IsDir() {
		return "", fmt.Errorf("cannot find %s to start Yozora", self)
	}
	return self, nil
}

func spawnDaemon(exe string) error {
	cmd := exec.Command(exe, "serve")
	cmd.Dir = filepath.Dir(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// spawnPanel starts the panel process for the tray. It must not pass
// HideWindow: the panel's first ShowWindow would honour SW_HIDE from the
// startup info and the window would never appear.
func spawnPanel(exe string) error {
	cmd := exec.Command(exe, "panel")
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
