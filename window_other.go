//go:build !darwin

package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// disableWindowAppearAnimation is a no-op off macOS: the implicit window
// appear animation being suppressed is an AppKit behavior.
func disableWindowAppearAnimation() {}

// presentWindow: off macOS there is no suspended-compositor stale-frame
// problem — a plain show is correct.
func presentWindow(ctx context.Context) {
	if ctx != nil {
		runtime.WindowShow(ctx)
	}
}
