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

	mu         sync.Mutex
	ready      bool // frontend subscribed and called Ready()
	pending    []string
	currentDoc string // last document opened or rendered; re-inlined on reload
	storeErr   error  // deferred settings-store init error, surfaced via GetSettings

	// launch starts an external command (system-default open, reveal in file
	// manager). Injectable so tests can assert what would be executed without
	// spawning anything.
	launch func(name string, arg ...string) error
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
	app.launch = app.startCommand
	return app
}

// startCommand is the default launch implementation: start the command,
// release it, and surface a failed exit as a UI error (never silently).
func (a *App) startCommand(name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Release the child; we don't care about its exit status but must not
	// leave a zombie.
	go func() {
		if err := cmd.Wait(); err != nil {
			a.emitError(fmt.Sprintf("%s failed: %v", name, err))
		}
	}()
	return nil
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
	a.currentDoc = abs
	ready, ctx := a.ready, a.ctx
	if !ready || ctx == nil {
		a.pending = append(a.pending, abs)
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	runtime.EventsEmit(ctx, EventDocOpen, abs)
}

// inlineDocPath picks the document to inline into the served shell: the first
// buffered open if any (Ready(inlinedPath) then skips re-delivering it),
// otherwise the current document (covers webview reloads).
func (a *App) inlineDocPath() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.pending) > 0 {
		return a.pending[0]
	}
	return a.currentDoc
}

func (a *App) emitError(msg string) {
	a.emit("app:error", msg)
}

func (a *App) emitNotice(msg string) {
	a.emit("app:notice", msg)
}

func (a *App) emit(event, msg string) {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx != nil {
		runtime.EventsEmit(ctx, event, msg)
	} else {
		fmt.Fprintln(os.Stderr, "md-view:", msg)
	}
}

// Ready is called by the frontend once it has subscribed to events.
// inlinedPath is the document path the frontend hydrated from the served
// shell ("" when nothing was inlined): the buffered open for that path is
// skipped (it is already rendered on screen), any open that arrived later is
// still delivered, and after a webview reload with nothing inlined or
// buffered the current document is re-delivered so it cannot be lost.
func (a *App) Ready(inlinedPath string) {
	a.mu.Lock()
	a.ready = true
	deliveries := readyDeliveries(inlinedPath, a.pending, a.currentDoc)
	a.pending = nil
	ctx := a.ctx
	a.mu.Unlock()
	if ctx != nil {
		// macOS: when the app is launched by opening a document, the window
		// server swallows the order-front issued during
		// applicationWillFinishLaunching — AppKit reports the window visible,
		// but it stays off screen for ~5 s (LaunchServices check-in timeout).
		// Re-asserting the show once the frontend is up makes it appear
		// immediately; it is a no-op when the window is already on screen.
		runtime.WindowShow(ctx)
	}
	for _, p := range deliveries {
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
	doc, err := a.renderer.RenderFile(resolved)
	if err != nil {
		return render.Doc{}, err
	}
	// Track the displayed document so a webview reload can restore it.
	a.mu.Lock()
	a.currentDoc = doc.Path
	a.mu.Unlock()
	return doc, nil
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

// systemOpenBlockedExts are extensions that indicate runnable content: opening
// them with the system default would execute or install them, not view them.
var systemOpenBlockedExts = map[string]bool{
	".command":     true,
	".sh":          true,
	".zsh":         true,
	".bash":        true,
	".tool":        true,
	".terminal":    true,
	".workflow":    true,
	".app":         true,
	".pkg":         true,
	".dmg":         true,
	".scpt":        true,
	".applescript": true,
	".jar":         true,
	".py":          true,
	".rb":          true,
	".pl":          true,
	// Windows-runnable (the +x check is inert there — Go reports no execute bits)
	".exe": true,
	".bat": true,
	".cmd": true,
	".msi": true,
	".vbs": true,
	".ps1": true,
	".scr": true,
	".hta": true,
	".lnk": true,
	".js":  true,
	".wsf": true,
	// macOS bundle directories (directories never trip the regular-file +x check)
	".prefpane":    true,
	".xpc":         true,
	".plugin":      true,
	".bundle":      true,
	".appex":       true,
	".qlgenerator": true,
	".saver":       true,
	".osax":        true,
}

// isBlockedForSystemOpen reports whether a file must not be handed to the OS
// default handler: any executable permission bit on a regular file, or an
// extension from the runnable-content denylist.
func isBlockedForSystemOpen(path string, info os.FileInfo) bool {
	if systemOpenBlockedExts[strings.ToLower(filepath.Ext(path))] {
		return true
	}
	return info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// OpenWithSystemDefault opens a scope-checked local file with the OS default
// application (PDFs, images, etc.). Files that look executable (any +x bit or
// a runnable-content extension) are never launched — the user gets a notice
// and the file is revealed in the file manager instead.
func (a *App) OpenWithSystemDefault(path string) error {
	resolved, err := a.scope.Check(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("open %s: %w", resolved, err)
	}
	if isBlockedForSystemOpen(resolved, info) {
		a.emitNotice(fmt.Sprintf("%s looks executable — revealing it in the file manager instead of opening it", filepath.Base(resolved)))
		return a.revealInFileManager(resolved)
	}
	var name string
	var args []string
	switch goruntime.GOOS {
	case "darwin":
		name, args = "open", []string{resolved}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", resolved}
	default:
		name, args = "xdg-open", []string{resolved}
	}
	if err := a.launch(name, args...); err != nil {
		return fmt.Errorf("open %s: %w", resolved, err)
	}
	return nil
}

// revealInFileManager shows the file in Finder/Explorer (selected) rather than
// opening it. On Linux the containing directory is opened.
func (a *App) revealInFileManager(resolved string) error {
	var name string
	var args []string
	switch goruntime.GOOS {
	case "darwin":
		name, args = "open", []string{"-R", resolved}
	case "windows":
		name, args = "explorer", []string{"/select," + resolved}
	default:
		name, args = "xdg-open", []string{filepath.Dir(resolved)}
	}
	if err := a.launch(name, args...); err != nil {
		return fmt.Errorf("reveal %s: %w", resolved, err)
	}
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

// assetMiddleware implements the two in-app routes: the initial shell request
// ("/" or "/index.html") is served with the initial state inlined (see
// inline.go), and /doc-asset/?p=<abs path> serves local images referenced by
// documents. Every asset request is validated against the scope.
func (a *App) assetMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isShellRequest(r) {
			a.serveShell(w, r, next)
			return
		}
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
