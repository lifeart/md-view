//go:build !darwin

package main

import "errors"

// PrewarmState mirrors the darwin definition so the bound method has the same
// shape on every platform; the login agent itself is macOS-only.
type PrewarmState struct {
	Supported     bool `json:"supported"`
	Enabled       bool `json:"enabled"`
	NeedsApproval bool `json:"needsApproval"`
}

func prewarmState() PrewarmState { return PrewarmState{} }

func setPrewarm(bool) error {
	return errors.New("keeping MDv ready in the background is macOS-only")
}
