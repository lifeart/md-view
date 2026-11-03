package main

import (
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
)

// appMenu is the macOS menu bar. Passing any menu replaces the one Wails
// builds when Menu is nil (App + Edit + Window), so those are rebuilt here
// from the same roles — dropping Edit would take Cmd+C with it — and the View
// menu slots in where the HIG puts it, before Window.
func appMenu(a *App) *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.EditMenu())
	view := m.AddSubmenu("View")
	view.AddText("Reload", keys.CmdOrCtrl("r"), func(*menu.CallbackData) { a.reload() })
	m.Append(menu.WindowMenu())
	return m
}
