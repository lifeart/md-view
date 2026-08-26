package main

import (
	"fmt"
	"os"

	"md-view/internal/render"
)

// A document open and the shell request that inlines it are ~65 ms apart on a
// cold launch (measured with scripts/perf-coldstart.sh: OnFileOpen at ~257 ms,
// the webview's shell request at ~322 ms). That gap is WebKit spinning up its
// first navigation, and Go spends all of it idle — while the render it is
// about to be asked for sits squarely on the critical path afterwards, costing
// 12 ms for a small document and 114 ms for a 2.7 MB one.
//
// So the render starts the moment the path is known, and whoever needs the
// document waits on that instead of starting its own. For anything under a few
// hundred KB the work is finished before the webview asks; for a large file the
// gap is filled and only the remainder is paid.
//
// This is also what makes the render *concurrent* for the first time — the
// background render can overlap a RenderDocument call from the frontend — so
// internal/render has TestRendererIsConcurrencySafe to keep that true.

// prerender is one document render, in flight or finished.
type prerender struct {
	path string
	// stamp identifies the file contents the render was started from, so a
	// document edited between the open and the shell request is re-read rather
	// than served stale.
	stamp string
	done  chan struct{}
	doc   *render.Doc
}

// fileStamp identifies a file version cheaply. Modification time plus size is
// what every build tool uses for this and is enough here: the window between
// starting the render and consuming it is milliseconds.
func fileStamp(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	return fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size()), true
}

// startPrerender begins rendering resolved in the background, replacing any
// previous pre-render. resolved must already have passed the scope check.
func (a *App) startPrerender(resolved string) {
	stamp, ok := fileStamp(resolved)
	if !ok {
		return
	}
	a.mu.Lock()
	if p := a.prerendered; p != nil && p.path == resolved && p.stamp == stamp {
		a.mu.Unlock() // already in flight or done for exactly this file
		return
	}
	p := &prerender{path: resolved, stamp: stamp, done: make(chan struct{})}
	a.prerendered = p
	a.mu.Unlock()

	go func() {
		defer close(p.done)
		doc, err := a.renderer.RenderFile(resolved)
		if err != nil {
			// Not reported here: whoever consumes the pre-render falls back to
			// a foreground render and reports the error in its own context
			// (the shell inliner logs it, RenderDocument returns it to the UI).
			return
		}
		p.doc = &doc
	}()
}

// takePrerender returns the pre-rendered document for resolved, waiting for an
// in-flight render to finish, or nil if there is none, it failed, or the file
// changed since it started. Reading p.doc after receiving from p.done is safe:
// closing the channel happens-after the write.
func (a *App) takePrerender(resolved string) *render.Doc {
	a.mu.Lock()
	p := a.prerendered
	a.mu.Unlock()
	if p == nil || p.path != resolved {
		return nil
	}
	<-p.done
	if p.doc == nil {
		return nil
	}
	if stamp, ok := fileStamp(resolved); !ok || stamp != p.stamp {
		return nil // edited while we were rendering it
	}
	return p.doc
}

// documentFor returns resolved's rendered document, using the pre-render when
// one is available and rendering in the foreground otherwise.
func (a *App) documentFor(resolved string) (render.Doc, error) {
	if doc := a.takePrerender(resolved); doc != nil {
		return *doc, nil
	}
	return a.renderer.RenderFile(resolved)
}
