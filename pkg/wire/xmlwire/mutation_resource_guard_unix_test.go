//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package xmlwire_test

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
)

const (
	mutationTestHeapLimit = 256 << 20
	mutationTestWallLimit = 2 * time.Second
)

func TestMain(testingMain *testing.M) {
	coverageProfile := os.Getenv("GOLIB_GREMLINS_COVERAGE_PROFILE")
	if coverageProfile == "" {
		os.Exit(testingMain.Run())
	}
	lockFile, err := os.OpenFile(coverageProfile+".xmlwire.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		failMutationTest("cannot open mutation test lock: %v", err)
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		_ = lockFile.Close()
		failMutationTest("cannot acquire mutation test lock: %v", err)
	}
	stop := make(chan struct{})
	go guardMutationTestHeap(stop)
	go guardMutationTestWallTime(stop)
	exitCode := testingMain.Run()
	close(stop)
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN); err != nil && exitCode == 0 {
		exitCode = 96
	}
	if err := lockFile.Close(); err != nil && exitCode == 0 {
		exitCode = 96
	}
	os.Exit(exitCode)
}

func guardMutationTestHeap(stop <-chan struct{}) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			var statistics runtime.MemStats
			runtime.ReadMemStats(&statistics)
			if statistics.HeapAlloc > mutationTestHeapLimit {
				failMutationTest("mutation test heap exceeded %d bytes", mutationTestHeapLimit)
			}
		}
	}
}

func guardMutationTestWallTime(stop <-chan struct{}) {
	timer := time.NewTimer(mutationTestWallLimit)
	defer timer.Stop()
	select {
	case <-stop:
		return
	case <-timer.C:
		failMutationTest("mutation test exceeded %s", mutationTestWallLimit)
	}
}

func failMutationTest(format string, arguments ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", arguments...)
	os.Exit(97)
}
