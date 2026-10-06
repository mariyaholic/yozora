//go:build windows

package main

import (
	"syscall"
	"testing"
)

func TestEmbeddedYozoraIconLoadsAsIconResource(t *testing.T) {
	module, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
	icon, _, err := syscall.NewLazyDLL("user32.dll").NewProc("LoadImageW").Call(module, 1, 1, 0, 0, 0x8040)
	if module == 0 || icon == 0 {
		t.Fatalf("LoadImageW IMAGE_ICON resource 1 failed: module=%#x err=%v", module, err)
	}
}
