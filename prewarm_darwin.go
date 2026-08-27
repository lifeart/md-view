//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -mmacosx-version-min=11.0
#cgo LDFLAGS: -framework Foundation -framework ServiceManagement

#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>
#import <ServiceManagement/ServiceManagement.h>

// The prewarm login agent, registered through SMAppService (macOS 13+).
//
// A cold launch cannot be made fast — see ARCHITECTURE.md, where AppKit and
// WebKit account for ~158 ms of floor before MDv runs a line. What it can be
// is rare: an agent that starts MDv hidden at login pays that cost once, when
// nobody is waiting, and every open afterwards is a warm one.
//
// SMAppService rather than writing into ~/Library/LaunchAgents: the item then
// appears in System Settings > General > Login Items under MDv's own name, and
// the reader can revoke it there. An app that installs a login item the system
// cannot attribute or the user cannot find is doing something they did not
// agree to.
//
// Status values mirror SMAppServiceStatus so Go can report the difference
// between "off" and "the reader turned this off in System Settings", which are
// not the same thing and must not be silently re-enabled.
enum {
	mdview_prewarm_unsupported = -1, // older than macOS 13
	mdview_prewarm_off = 0,
	mdview_prewarm_on = 1,
	mdview_prewarm_denied = 2, // disabled by the user in System Settings
	mdview_prewarm_needs_approval = 3
};

static SMAppService *mdview_agent(void) API_AVAILABLE(macos(13.0)) {
	return [SMAppService agentServiceWithPlistName:@"com.wails.md-view.prewarm.plist"];
}

static int mdview_prewarm_status(void) {
	if (@available(macOS 13.0, *)) {
		switch (mdview_agent().status) {
		case SMAppServiceStatusEnabled:
			return mdview_prewarm_on;
		case SMAppServiceStatusRequiresApproval:
			return mdview_prewarm_needs_approval;
		case SMAppServiceStatusNotFound:
			return mdview_prewarm_off;
		default:
			return mdview_prewarm_off;
		}
	}
	return mdview_prewarm_unsupported;
}

// Returns NULL on success, or a copy of the failure reason the caller frees.
static char *mdview_prewarm_set(int enable) {
	if (@available(macOS 13.0, *)) {
		NSError *err = nil;
		BOOL ok = enable ? [mdview_agent() registerAndReturnError:&err]
		                 : [mdview_agent() unregisterAndReturnError:&err];
		if (ok) {
			return NULL;
		}
		const char *msg = err.localizedDescription.UTF8String;
		return strdup(msg ? msg : "unknown error");
	}
	return strdup("Keeping MDv ready requires macOS 13 or later.");
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

// PrewarmState is what the frontend needs to render the control honestly.
type PrewarmState struct {
	// Supported is false on macOS 12 and earlier, where the toggle is hidden
	// rather than shown broken.
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`
	// NeedsApproval: registered, but the reader has to allow it in System
	// Settings before it will run. Saying "on" here would be a lie.
	NeedsApproval bool `json:"needsApproval"`
}

func prewarmState() PrewarmState {
	switch C.mdview_prewarm_status() {
	case C.mdview_prewarm_on:
		return PrewarmState{Supported: true, Enabled: true}
	case C.mdview_prewarm_needs_approval:
		return PrewarmState{Supported: true, Enabled: true, NeedsApproval: true}
	case C.mdview_prewarm_unsupported:
		return PrewarmState{}
	default:
		return PrewarmState{Supported: true}
	}
}

func setPrewarm(enable bool) error {
	var flag C.int
	if enable {
		flag = 1
	}
	msg := C.mdview_prewarm_set(flag)
	if msg == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(msg))
	return errors.New(C.GoString(msg))
}
