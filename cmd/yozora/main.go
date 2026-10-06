package main

import (
	"os"
	"syscall"

	"uika-resonance/internal/cli"
)

func main() {
	if len(os.Args) > 1 {
		attachParentConsole()
		os.Exit(cli.Main(os.Args[1:]))
	}
	os.Exit(cli.Dashboard())
}

func attachParentConsole() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	if r, _, _ := kernel32.NewProc("AttachConsole").Call(^uintptr(0)); r == 0 {
		return
	}
	if h, err := syscall.Open("CONOUT$", syscall.O_RDWR, 0); err == nil {
		f := os.NewFile(uintptr(h), "CONOUT$")
		os.Stdout, os.Stderr = f, f
	}
	if h, err := syscall.Open("CONIN$", syscall.O_RDWR, 0); err == nil {
		os.Stdin = os.NewFile(uintptr(h), "CONIN$")
	}
}
