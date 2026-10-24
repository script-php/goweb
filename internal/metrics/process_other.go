//go:build !linux

package metrics

import (
	"runtime"
	"time"
)

func processStats() (uint64, time.Duration) {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	return memory.Sys, 0
}
