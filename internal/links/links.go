// Package links resolves and classifies document links and enforces the
// filesystem scope described in ARCHITECTURE.md's security model.
package links

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
)

// Kind classifies a resolved link.
type Kind string

const (
	// KindAnchor is an in-document #fragment link.
	KindAnchor Kind = "anchor"
	// KindMarkdown is a link to a local markdown file (optionally with a fragment).
	KindMarkdown Kind = "markdown"
	// KindExternal is an http(s) URL to open in the default browser.
	KindExternal Kind = "external"
	// KindFile is a link to a local non-markdown file (PDF, etc.).
	KindFile Kind = "file"
	// KindUnsupported is anything else (javascript:, mailto:, unknown schemes).
	KindUnsupported Kind = "unsupported"
)

// Resolution is the result of classifying a clicked href.
type Resolution struct {
	Kind     Kind   `json:"kind"`
	Path     string `json:"path"`     // absolute local path (markdown/file kinds)
	Fragment string `json:"fragment"` // anchor within the target (anchor/markdown kinds)
	URL      string `json:"url"`      // original URL (external kind)
}

// markdownExts are the extensions treated as markdown documents.
var markdownExts = map[string]bool{
	".md":       true,
	".markdown": true,
	".mdown":    true,
	".mkd":      true,
}

// IsMarkdownPath reports whether path has a markdown extension.
func IsMarkdownPath(path string) bool {
	return markdownExts[strings.ToLower(filepath.Ext(path))]
}

// Resolve classifies href clicked inside the document at basePath.
// basePath must be an absolute path to the current document.
func Resolve(basePath, href string) (Resolution, error) {
	href = strings.TrimSpace(href)
	if href == "" {
		return Resolution{}, fmt.Errorf("empty link")
	}
	if strings.HasPrefix(href, "#") {
		return Resolution{Kind: KindAnchor, Fragment: strings.TrimPrefix(href, "#")}, nil
	}

	u, err := url.Parse(href)
	if err != nil {
		return Resolution{}, fmt.Errorf("unparseable link %q: %w", href, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return Resolution{Kind: KindExternal, URL: href}, nil
	case "":
		if u.Host != "" {
			// Scheme-relative URL (//host/path) — never a local path.
			return Resolution{Kind: KindUnsupported, URL: href}, nil
		}
		// relative or rootful local path — handled below
	case "file":
		if u.Path == "" {
			return Resolution{Kind: KindUnsupported, URL: href}, nil
		}
		return resolveLocal(basePath, u.Path, u.Fragment)
	default:
		// javascript:, mailto:, data:, vbscript:, custom schemes — never followed.
		return Resolution{Kind: KindUnsupported, URL: href}, nil
	}

	pathPart := href
	fragment := ""
	if i := strings.Index(pathPart, "#"); i >= 0 {
		fragment = pathPart[i+1:]
		pathPart = pathPart[:i]
	}
	if i := strings.Index(pathPart, "?"); i >= 0 {
		pathPart = pathPart[:i]
	}
	if decoded, err := url.PathUnescape(pathPart); err == nil {
		pathPart = decoded
	}
	if pathPart == "" {
		// e.g. "?query#frag" — treat as in-doc anchor if a fragment exists.
		if fragment != "" {
			return Resolution{Kind: KindAnchor, Fragment: fragment}, nil
		}
		return Resolution{Kind: KindUnsupported, URL: href}, nil
	}
	return resolveLocal(basePath, pathPart, fragment)
}

func resolveLocal(basePath, pathPart, fragment string) (Resolution, error) {
	abs := pathPart
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(filepath.Dir(basePath), abs)
	}
	abs = filepath.Clean(abs)
	if IsMarkdownPath(abs) {
		return Resolution{Kind: KindMarkdown, Path: abs, Fragment: fragment}, nil
	}
	return Resolution{Kind: KindFile, Path: abs}, nil
}

// Scope is the set of directory trees the app is allowed to read from: the
// directory of every document the user has opened (extended per navigation).
// Every file read and every /doc-asset/ request must be validated against it.
type Scope struct {
	mu   sync.RWMutex
	dirs []string // absolute, symlink-resolved, cleaned
}

// NewScope returns an empty scope.
func NewScope() *Scope {
	return &Scope{}
}

// AddDir adds dir (and everything beneath it) to the allowed scope.
// The directory must exist; symlinks are resolved before storing.
func (s *Scope) AddDir(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("scope add %q: %w", dir, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return fmt.Errorf("scope add %q: %w", dir, err)
	}
	resolved = filepath.Clean(resolved)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.dirs {
		if d == resolved {
			return nil
		}
	}
	s.dirs = append(s.dirs, resolved)
	return nil
}

// AddFile adds the parent directory of file to the scope.
func (s *Scope) AddFile(path string) error {
	return s.AddDir(filepath.Dir(path))
}

// Check validates that path (which must exist) lies inside one of the allowed
// directory trees after cleaning and symlink resolution. It returns the fully
// resolved path on success.
func (s *Scope) Check(path string) (string, error) {
	if strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("invalid path")
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("access denied: %q: %w", path, err)
	}
	// Resolve symlinks so a link inside an allowed dir cannot smuggle reads
	// from outside of it.
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("access denied: %q: %w", path, err)
	}
	resolved = filepath.Clean(resolved)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, dir := range s.dirs {
		if resolved == dir || strings.HasPrefix(resolved, dir+string(filepath.Separator)) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("access denied: %q is outside the opened documents' scope", path)
}
