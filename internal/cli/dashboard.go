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

func Dashboard() int {
	hideLauncherConsole()
	owner, release, err := sysutil.AcquireNamedMutex(launcherSingletonName)
	if err != nil {
		launcherNotify("Yozora", "Could not start: "+err.Error(), notifyError)
		return 1
	}
	if !owner {
		return activateRunningPanel()
	}
	defer release()
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
