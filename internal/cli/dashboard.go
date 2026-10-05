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
)

// dashboardLaunchTimeout bounds waiting for a freshly spawned daemon.
const dashboardLaunchTimeout = 25 * time.Second

// Dashboard is the double-click entry point: it ensures the control panel is
// available (starting the daemon when needed) and shows it in a native
// WebView2 window, falling back to the default browser.
func Dashboard() int {
	hideLauncherConsole()
	cfgPath, dataDir, _ := config.Paths()
	if err := ensureDashboardConfig(cfgPath); err != nil {
		launcherNotify("Yozora", "Could not prepare the settings file:\n"+err.Error(), notifyError)
		return 1
	}
	endpointPath := dashboardEndpointPath(dataDir)
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

// daemonExecutable finds the daemon next to the launcher, preferring the same
// machine's current binary. When missing, plain `Yozora.exe serve` still
// works because this binary can serve too.
func daemonExecutable() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(self)
	candidate := filepath.Join(dir, "uika-resonance.exe")
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		return candidate, nil
	}
	if st, err := os.Stat(self); err == nil && !st.IsDir() {
		return self, nil
	}
	return "", fmt.Errorf("no daemon executable found next to %s", self)
}

func spawnDaemon(exe string) error {
	cmd := exec.Command(exe, "serve")
	cmd.Dir = filepath.Dir(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW: no console flash
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
