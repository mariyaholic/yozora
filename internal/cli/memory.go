//go:build windows

package cli

import (
	"runtime"
	"runtime/debug"
	"time"
)

const (
	daemonGCPercent   = 20
	daemonMemoryLimit = 48 << 20
	daemonScavengeGap = time.Minute
)

func capRuntimeMemory() {
	debug.SetGCPercent(daemonGCPercent)
	debug.SetMemoryLimit(daemonMemoryLimit)
	runtime.GOMAXPROCS(min(runtime.NumCPU(), 2))
	go func() {
		for range time.Tick(daemonScavengeGap) {
			debug.FreeOSMemory()
		}
	}()
}
