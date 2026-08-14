//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

// AppKit gives document-class windows an implicit appear/disappear zoom
// (NSWindowAnimationBehaviorDocumentWindow). On a document-open launch that
// animation starts *after* the window's frame is already on screen, which
// reads as a flash followed by a laggy unfold. The app's identity is
// "instant": present the window in a single frame instead.
static void mdview_disable_window_animation(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		for (NSWindow *w in [NSApp windows]) {
			w.animationBehavior = NSWindowAnimationBehaviorNone;
		}
	});
}

// Two-phase warm-open presentation. A hidden window's WKWebView compositor is
// suspended: the DOM swap is committed in the web process, but the UI-side
// layer tree still holds the previous document, so ordering the window front
// directly presents 1-2 frames of stale content. Phase 1 orders the window
// front at an imperceptible alpha — on screen, so WebKit unthrottles and
// applies the pending commit — and phase 2 (driven by the frontend after a
// painted frame) restores full alpha. A 300 ms failsafe guarantees the window
// can never be left transparent.
static void mdview_present_begin(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		NSWindow *w = [[NSApp windows] firstObject];
		if (w == nil) {
			return;
		}
		bool onScreen = w.isVisible && (w.occlusionState & NSWindowOcclusionStateVisible);
		if (!onScreen) {
			w.alphaValue = 0.01;
			dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(300 * NSEC_PER_MSEC)),
				dispatch_get_main_queue(), ^{
				w.alphaValue = 1.0;
			});
		}
		[w makeKeyAndOrderFront:nil];
		[NSApp activateIgnoringOtherApps:YES];
	});
}

static void mdview_present_finish(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		NSWindow *w = [[NSApp windows] firstObject];
		if (w != nil) {
			w.alphaValue = 1.0;
		}
	});
}
*/
import "C"

import "context"

// disableWindowAppearAnimation turns off AppKit's implicit window appear/
// disappear animation for every app window. Queued on the main thread; call
// before the first WindowShow (both are FIFO on the main queue, so ordering
// holds).
func disableWindowAppearAnimation() {
	C.mdview_disable_window_animation()
}

// presentWindowBegin orders the window front invisibly (alpha 0.01) so the
// webview's pending content commit lands before anything is shown. ctx is
// unused on macOS — the native side owns the window.
func presentWindowBegin(_ context.Context) {
	C.mdview_present_begin()
}

// presentWindowFinish restores full alpha once the frontend has confirmed a
// painted frame of the new content.
func presentWindowFinish() {
	C.mdview_present_finish()
}
