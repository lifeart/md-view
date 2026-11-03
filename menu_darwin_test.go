package main

import (
	"testing"

	"github.com/wailsapp/wails/v2/pkg/menu"
)

// The menu must keep what Wails' default provided — App, Edit (Cmd+C), Window —
// and add View > Reload on Cmd+R.
func TestAppMenu(t *testing.T) {
	m := appMenu(NewApp())
	if len(m.Items) != 4 {
		t.Fatalf("got %d top-level items, want 4", len(m.Items))
	}
	for i, role := range map[int]menu.Role{0: menu.AppMenuRole, 1: menu.EditMenuRole, 3: menu.WindowMenuRole} {
		if m.Items[i].Role != role {
			t.Errorf("item %d: role %v, want %v", i, m.Items[i].Role, role)
		}
	}
	view := m.Items[2]
	if view.Label != "View" || view.SubMenu == nil || len(view.SubMenu.Items) != 1 {
		t.Fatalf("item 2 is not a View menu with one entry: %+v", view)
	}
	reload := view.SubMenu.Items[0]
	if reload.Label != "Reload" || reload.Accelerator == nil ||
		reload.Accelerator.Key != "r" || len(reload.Accelerator.Modifiers) != 1 {
		t.Errorf("Reload item wrong: %+v (accelerator %+v)", reload, reload.Accelerator)
	}
	if reload.Click == nil {
		t.Fatal("Reload has no click handler")
	}
	// Before startup there is no frontend to notify; clicking must not panic.
	reload.Click(&menu.CallbackData{MenuItem: reload})
}
