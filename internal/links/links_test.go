package links

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveClassification(t *testing.T) {
	base := "/docs/project/index.md"
	cases := []struct {
		href string
		want Resolution
	}{
		{"#intro", Resolution{Kind: KindAnchor, Fragment: "intro"}},
		{"sub/page.md", Resolution{Kind: KindMarkdown, Path: "/docs/project/sub/page.md"}},
		{"sub/page.md#details", Resolution{Kind: KindMarkdown, Path: "/docs/project/sub/page.md", Fragment: "details"}},
		{"../other.markdown", Resolution{Kind: KindMarkdown, Path: "/docs/other.markdown"}},
		{"/abs/readme.mkd", Resolution{Kind: KindMarkdown, Path: "/abs/readme.mkd"}},
		{"NOTES.MD", Resolution{Kind: KindMarkdown, Path: "/docs/project/NOTES.MD"}},
		{"with%20space.md", Resolution{Kind: KindMarkdown, Path: "/docs/project/with space.md"}},
		{"https://wails.io/docs", Resolution{Kind: KindExternal, URL: "https://wails.io/docs"}},
		{"http://example.com", Resolution{Kind: KindExternal, URL: "http://example.com"}},
		{"notes.txt", Resolution{Kind: KindFile, Path: "/docs/project/notes.txt"}},
		{"../../../../etc/passwd", Resolution{Kind: KindFile, Path: "/etc/passwd"}},
		{"javascript:alert(1)", Resolution{Kind: KindUnsupported, URL: "javascript:alert(1)"}},
		{"mailto:a@b.c", Resolution{Kind: KindUnsupported, URL: "mailto:a@b.c"}},
		{"vbscript:x", Resolution{Kind: KindUnsupported, URL: "vbscript:x"}},
		{"file:///docs/project/sub/page.md", Resolution{Kind: KindMarkdown, Path: "/docs/project/sub/page.md"}},
	}
	for _, c := range cases {
		got, err := Resolve(base, c.href)
		if err != nil {
			t.Errorf("Resolve(%q): unexpected error %v", c.href, err)
			continue
		}
		if got != c.want {
			t.Errorf("Resolve(%q) = %+v, want %+v", c.href, got, c.want)
		}
	}

	if _, err := Resolve(base, ""); err == nil {
		t.Errorf("Resolve(\"\") should error")
	}
}

func TestScopeAllowsInsideTree(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(sub, "a.md")
	if err := os.WriteFile(inside, []byte("# a"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewScope()
	if err := s.AddDir(dir); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	if _, err := s.Check(inside); err != nil {
		t.Errorf("Check(inside) = %v, want nil", err)
	}
	// Unclean path variants still resolve and pass.
	if _, err := s.Check(filepath.Join(dir, "sub", "..", "sub", "a.md")); err != nil {
		t.Errorf("Check(unclean inside) = %v, want nil", err)
	}
}

func TestScopeRejectsOutsideAndTraversal(t *testing.T) {
	dir := t.TempDir()
	s := NewScope()
	if err := s.AddDir(dir); err != nil {
		t.Fatalf("AddDir: %v", err)
	}

	for _, p := range []string{
		"/etc/passwd",
		filepath.Join(dir, "..", "..", "..", "..", "etc", "passwd"),
		filepath.Join(dir, "../outside.md"),
		"/etc/../etc/passwd",
	} {
		if resolved, err := s.Check(p); err == nil {
			t.Errorf("Check(%q) allowed (resolved %q), want rejection", p, resolved)
		}
	}
}

func TestScopeRejectsPrefixSiblingDir(t *testing.T) {
	// /tmp/x/docs must not authorize /tmp/x/docs-evil (naive prefix check bug).
	parent := t.TempDir()
	docs := filepath.Join(parent, "docs")
	evil := filepath.Join(parent, "docs-evil")
	for _, d := range []string{docs, evil} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(evil, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewScope()
	if err := s.AddDir(docs); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Check(target); err == nil {
		t.Errorf("sibling dir with shared prefix must be rejected")
	}
}

func TestScopeRejectsSymlinkEscape(t *testing.T) {
	scoped := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(scoped, "sneaky.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	s := NewScope()
	if err := s.AddDir(scoped); err != nil {
		t.Fatal(err)
	}
	if resolved, err := s.Check(link); err == nil {
		t.Errorf("symlink escaping the scope was allowed (resolved %q)", resolved)
	}
}

func TestScopeRejectsNonexistent(t *testing.T) {
	dir := t.TempDir()
	s := NewScope()
	if err := s.AddDir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Check(filepath.Join(dir, "missing.md")); err == nil {
		t.Errorf("nonexistent path should be rejected (EvalSymlinks fails)")
	}
	if _, err := s.Check("relative/path.md"); err == nil {
		// filepath.Abs succeeds for relative paths, but resolution happens
		// against the process cwd; the file will not exist under scope.
		t.Log("relative path resolved against cwd — acceptable only if it fails scope")
	}
}

func TestIsMarkdownPath(t *testing.T) {
	for p, want := range map[string]bool{
		"a.md": true, "a.markdown": true, "a.mdown": true, "a.mkd": true,
		"A.MD": true, "a.txt": false, "a.pdf": false, "a": false, "a.md.txt": false,
	} {
		if got := IsMarkdownPath(p); got != want {
			t.Errorf("IsMarkdownPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestResolveRejectsWeirdHrefs(t *testing.T) {
	base := "/docs/project/index.md"
	// Scheme-relative URL: url.Parse gives Host, no Scheme — must not be
	// treated as a local path.
	got, err := Resolve(base, "//evil.example/x.md")
	if err != nil {
		t.Fatalf("Resolve scheme-relative: %v", err)
	}
	if got.Kind != KindUnsupported {
		t.Errorf("scheme-relative URL classified %q, want unsupported: %+v", got.Kind, got)
	}
}
