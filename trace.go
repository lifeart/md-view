package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Launch tracing, off unless MDVIEW_TRACE names a file to append to. Kept in
// the tree because the macOS launch path can only be timed from inside the
// process: LaunchServices launches give no stderr to attach to, so the trace
// goes to a file the harness can read.
var (
	traceMu   sync.Mutex
	traceFile *os.File
)

func init() {
	path := os.Getenv("MDVIEW_TRACE")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "md-view: cannot open trace file %s: %v\n", path, err)
		return
	}
	traceFile = f
	tracef("process start")
}

// tracef records a timestamped event. Absolute epoch milliseconds, so the
// external launch harness can subtract its own launch timestamp.
func tracef(format string, args ...any) {
	if traceFile == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	traceMu.Lock()
	defer traceMu.Unlock()
	fmt.Fprintf(traceFile, "%.1f %s\n", float64(time.Now().UnixNano())/1e6, msg)
}
