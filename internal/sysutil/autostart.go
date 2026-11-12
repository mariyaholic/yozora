//go:build windows

package sysutil

import (
	"path/filepath"
	"strings"
)

// autostartCommand is the HKCU Run data that starts exe's daemon at sign-in.
func autostartCommand(exe string) string { return `"` + exe + `" serve` }

// splitRunCommand separates a Run command into its executable path and
// arguments. Quoted and bare paths are both accepted, since other tools or
// older builds may have written either form.
func splitRunCommand(cmd string) (exe, args string) {
	cmd = strings.TrimSpace(cmd)
	if rest, ok := strings.CutPrefix(cmd, `"`); ok {
		if i := strings.IndexByte(rest, '"'); i >= 0 {
			return rest[:i], strings.TrimSpace(rest[i+1:])
		}
		return rest, ""
	}
	if i := strings.IndexByte(cmd, ' '); i >= 0 {
		return cmd[:i], strings.TrimSpace(cmd[i+1:])
	}
	return cmd, ""
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// withinDir reports whether path lies inside dir (case-insensitive, as NTFS is).
func withinDir(path, dir string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(strings.ToLower(filepath.Clean(dir)), strings.ToLower(filepath.Clean(path)))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// autostartNeedsRewrite decides whether a registered Run command should be
// replaced with one for exe. Binaries under the temp directory (go run, test
// binaries) never claim autostart, so a developer run cannot repoint a real
// install at a file that is about to be deleted.
func autostartNeedsRewrite(registered, exe, tempDir string) bool {
	if strings.TrimSpace(exe) == "" || withinDir(exe, tempDir) {
		return false
	}
	path, args := splitRunCommand(registered)
	return !samePath(path, exe) || !strings.EqualFold(args, "serve")
}
