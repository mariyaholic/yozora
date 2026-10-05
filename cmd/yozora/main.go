// Yozora.exe — double-click control panel for the Yozora daemon.
// No arguments opens the settings window; arguments are forwarded to the
// normal command surface (serve, status, doctor, ...), printing to the
// launching terminal when one exists.
//
// Built as a GUI-subsystem binary: double-click launches never create a
// console window. Terminal invocations re-attach to the parent console.
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

// attachParentConsole wires stdio to the console this process was launched
// from. It fails silently on double-click launches (no parent console), so
// the launcher stays window-only.
func attachParentConsole() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	if r, _, _ := kernel32.NewProc("AttachConsole").Call(^uintptr(0)); r == 0 { // ATTACH_PARENT_PROCESS
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
