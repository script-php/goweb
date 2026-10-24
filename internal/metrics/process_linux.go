//go:build linux

package metrics

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func processStats() (uint64, time.Duration) {
	var usage syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &usage)
	cpu := time.Duration(usage.Utime.Sec+usage.Stime.Sec)*time.Second + time.Duration(usage.Utime.Usec+usage.Stime.Usec)*time.Microsecond

	contents, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return uint64(usage.Maxrss) * 1024, cpu
	}
	fields := strings.Fields(string(contents))
	if len(fields) < 2 {
		return uint64(usage.Maxrss) * 1024, cpu
	}
	residentPages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return uint64(usage.Maxrss) * 1024, cpu
	}
	return residentPages * uint64(os.Getpagesize()), cpu
}
