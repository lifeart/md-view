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

// Present the window with fresh content. A hidden window's WKWebView
// compositor is suspended: the frontend's DOM swap is committed in the web
// process, but the UI-side layer tree still holds the previously displayed
// document, so ordering the window front directly presents 1-2 frames of
// stale content. There is no public "show when the pending commit has been
// presented" API, so the presentation is gated: order the window front at an
// imperceptible alpha — on screen, which unthrottles WebKit and lets it apply
// the pending commit (typically the next frame) — and restore full alpha
// 50 ms (3 frames) later. Windows that are already on screen skip the gate
// entirely (in-place navigation must not blink).
static void mdview_present_window(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		NSWindow *w = [[NSApp windows] firstObject];
		if (w == nil) {
			return;
		}
		if (w.miniaturized) {
			[w deminiaturize:nil];
		}
		if (!(w.isVisible && (w.occlusionState & NSWindowOcclusionStateVisible))) {
			w.alphaValue = 0.01;
			dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(50 * NSEC_PER_MSEC)),
				dispatch_get_main_queue(), ^{
				w.alphaValue = 1.0;
			});
		}
		[w makeKeyAndOrderFront:nil];
		[NSApp activateIgnoringOtherApps:YES];
	});
}

// Whether the window is genuinely off screen (hidden app, closed-to-hidden,
// miniaturized) as opposed to merely occluded by another window. Synchronous
// main-queue read — must never be called from the main thread.
static int mdview_window_hidden(void) {
	__block int hidden = 0;
	dispatch_sync(dispatch_get_main_queue(), ^{
		NSWindow *w = [[NSApp windows] firstObject];
		hidden = (w == nil || !w.isVisible || [NSApp isHidden]) ? 1 : 0;
	});
	return hidden;
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

// presentWindow shows the window gated on the pending content commit (see the
// C comment above). ctx is unused on macOS — the native side owns the window.
func presentWindow(_ context.Context) {
	C.mdview_present_window()
}

// windowHidden reports whether the window is genuinely off screen. Runs a
// synchronous main-queue hop — only call from non-main goroutines (Wails
// bound methods qualify).
func windowHidden() bool {
	return C.mdview_window_hidden() != 0
}
