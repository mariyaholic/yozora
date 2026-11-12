//go:build windows

package sysutil

import "testing"

// A fresh CI profile may have any region; only the shape is checked.
func TestUserRegionDoesNotPanic(t *testing.T) {
	t.Logf("region %q", UserRegion())
}
