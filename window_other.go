//go:build !darwin

package main

// disableWindowAppearAnimation is a no-op off macOS: the implicit window
// appear animation being suppressed is an AppKit behavior.
func disableWindowAppearAnimation() {}
