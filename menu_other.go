//go:build !darwin

package main

import "github.com/wailsapp/wails/v2/pkg/menu"

// appMenu: off macOS a non-nil menu becomes a menu bar inside the window,
// which would be chrome on a reading surface. Keep none.
func appMenu(*App) *menu.Menu { return nil }
