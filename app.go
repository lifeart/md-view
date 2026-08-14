package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"

	"context"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"md-view/internal/links"
	"md-view/internal/render"
	"md-view/internal/settings"
)

// EventDocOpen is emitted to the frontend with a file path (string) whenever
// the OS or the Go side asks the app to open a document.
const EventDocOpen = "doc:open"

// App is the Wails-bound application core.
type App struct {
	ctx      context.Context
	renderer *render.Renderer
	scope    *links.Scope
	store    *settings.Store

	mu       sync.Mutex
	ready    bool // frontend subscribed and called Ready()
	pending  []string
	storeErr error // deferred settings-store init error, surfaced via GetSettings
}

// NewApp creates the application core.
func NewApp() *App {
	app := &App{
		renderer: render.New(),
		scope:    links.NewScope(),
	}
	store, err := settings.NewStore()
	if err != nil {
		// Don't crash on a missing config dir; report it when the frontend asks.
		app.storeErr = err
	} else {
		app.store = store
	}
	return app
}

// startup is the Wails OnStartup hook.
func (a *App) startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
	// Native drag-and-drop: deliver dropped markdown files as document opens.
	runtime.OnFileDrop(ctx, func(x, y int, paths []string) {
		for _, p := range paths {
			if links.IsMarkdownPath(p) {
				a.openPath(p)
				return
			}
		}
		if len(paths) > 0 {
			runtime.EventsEmit(ctx, "app:notice", "Only markdown files can be opened here")
		}
	})
}

// openPath is the single trusted entry point for opening a document: OS file
// association, CLI argv, second instance, drag-and-drop, Cmd+O. It extends the
// filesystem scope and delivers the path to the frontend (buffering until the
// frontend has called Ready).
func (a *App) openPath(path string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		a.emitError(fmt.Sprintf("Cannot resolve %q: %v", path, err))
		return
	}
	if err := a.scope.AddFile(abs); err != nil {
		a.emitError(fmt.Sprintf("Cannot open %q: %v", path, err))
		return
	}
	a.mu.Lock()
	ready, ctx := a.ready, a.ctx
	if !ready || ctx == nil {
		a.pending = append(a.pending, abs)
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	runtime.EventsEmit(ctx, EventDocOpen, abs)
}

func (a *App) emitError(msg string) {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx != nil {
		runtime.EventsEmit(ctx, "app:error", msg)
	} else {
		fmt.Fprintln(os.Stderr, "md-view:", msg)
	}
}

// Ready is called by the frontend once it has subscribed to events; buffered
// open requests (OnFileOpen can fire before the webview exists) are flushed.
func (a *App) Ready() {
	a.mu.Lock()
	a.ready = true
	pending := a.pending
	a.pending = nil
	ctx := a.ctx
	a.mu.Unlock()
	for _, p := range pending {
		runtime.EventsEmit(ctx, EventDocOpen, p)
	}
}

// RenderDocument renders a markdown file. The path must already be inside the
// allowed scope (established by OpenPath or ResolveLink) — this is the only
// place document bytes are read for display.
func (a *App) RenderDocument(path string) (render.Doc, error) {
	resolved, err := a.scope.Check(path)
	if err != nil {
		return render.Doc{}, err
	}
	if !links.IsMarkdownPath(resolved) {
		return render.Doc{}, fmt.Errorf("not a markdown file: %s", resolved)
	}
	return a.renderer.RenderFile(resolved)
}

// ResolveLink classifies href clicked inside the document at basePath.
// For markdown targets that exist, the target's directory is added to the
// scope (navigation extends scope, per the architecture's security model).
func (a *App) ResolveLink(basePath, href string) (links.Resolution, error) {
	if _, err := a.scope.Check(basePath); err != nil {
		return links.Resolution{}, fmt.Errorf("resolve link: base document out of scope: %w", err)
	}
	res, err := links.Resolve(basePath, href)
	if err != nil {
		return links.Resolution{}, err
	}
	if res.Kind == links.KindMarkdown {
		if _, err := os.Stat(res.Path); err != nil {
			return links.Resolution{}, fmt.Errorf("file not found: %s", res.Path)
		}
		if err := a.scope.AddFile(res.Path); err != nil {
			return links.Resolution{}, fmt.Errorf("cannot open %s: %w", res.Path, err)
		}
	}
	return res, nil
}

// GetSettings returns the persisted settings (defaults when unset).
func (a *App) GetSettings() (settings.Settings, error) {
	if a.store == nil {
		return settings.Default(), fmt.Errorf("settings unavailable: %v", a.storeErr)
	}
	return a.store.Load()
}

// SetSettings persists the given settings.
func (a *App) SetSettings(s settings.Settings) error {
	if a.store == nil {
		return fmt.Errorf("settings unavailable: %v", a.storeErr)
	}
	return a.store.Save(s)
}

// OpenFileDialog shows the native open dialog filtered to markdown files and
// returns the chosen path ("" when cancelled). The chosen file's directory is
// added to the scope so the frontend can render it.
func (a *App) OpenFileDialog() (string, error) {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return "", fmt.Errorf("application not started yet")
	}
	path, err := runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title: "Open Markdown File",
		Filters: []runtime.FileFilter{
			{DisplayName: "Markdown (*.md, *.markdown, *.mdown, *.mkd)", Pattern: "*.md;*.markdown;*.mdown;*.mkd"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("open dialog: %w", err)
	}
	if path == "" {
		return "", nil
	}
	if err := a.scope.AddFile(path); err != nil {
		return "", fmt.Errorf("cannot open %s: %w", path, err)
	}
	return path, nil
}

// OpenExternal opens an http(s) URL in the default browser. Other schemes are
// rejected — external links never load in-app.
func (a *App) OpenExternal(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("refusing to open non-http(s) URL %q", rawURL)
	}
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return fmt.Errorf("application not started yet")
	}
	runtime.BrowserOpenURL(ctx, u.String())
	return nil
}

// OpenWithSystemDefault opens a scope-checked local file with the OS default
// application (PDFs, images, etc.).
func (a *App) OpenWithSystemDefault(path string) error {
	resolved, err := a.scope.Check(path)
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		cmd = exec.Command("open", resolved)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", resolved)
	default:
		cmd = exec.Command("xdg-open", resolved)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open %s: %w", resolved, err)
	}
	// Release the child; we don't care about its exit status but must not
	// leave a zombie.
	go func() {
		if err := cmd.Wait(); err != nil {
			a.emitError(fmt.Sprintf("System open failed for %s: %v", resolved, err))
		}
	}()
	return nil
}

// onSecondInstanceLaunch routes a repeat app launch (Windows/Linux, or any
// second CLI invocation) into the running instance.
func (a *App) onSecondInstanceLaunch(data options.SecondInstanceData) {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx != nil {
		runtime.WindowUnminimise(ctx)
		runtime.Show(ctx)
	}
	for _, arg := range data.Args {
		p := arg
		if !filepath.IsAbs(p) {
			p = filepath.Join(data.WorkingDirectory, p)
		}
		if links.IsMarkdownPath(p) {
			if _, err := os.Stat(p); err == nil {
				a.openPath(p)
				return
			}
		}
	}
}

// assetMiddleware serves /doc-asset/?p=<abs path> requests for local images
// referenced by documents. Every request is validated against the scope.
func (a *App) assetMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, render.AssetRoutePrefix) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := r.URL.Query().Get("p")
		if p == "" {
			http.Error(w, "missing p parameter", http.StatusBadRequest)
			return
		}
		resolved, err := a.scope.Check(p)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		info, err := os.Stat(resolved)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, resolved)
	})
}

// initialFileFromArgs picks the first existing markdown file from CLI args
// (Windows/Linux file associations, and a dev convenience on macOS).
func initialFileFromArgs(args []string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if !links.IsMarkdownPath(arg) {
			continue
		}
		if abs, err := filepath.Abs(arg); err == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs
			}
		}
	}
	return ""
}
