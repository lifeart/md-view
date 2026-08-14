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
*/
import "C"

// disableWindowAppearAnimation turns off AppKit's implicit window appear/
// disappear animation for every app window. Queued on the main thread; call
// before the first WindowShow (both are FIFO on the main queue, so ordering
// holds).
func disableWindowAppearAnimation() {
	C.mdview_disable_window_animation()
}
