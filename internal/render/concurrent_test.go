package render

import (
	"strings"
	"sync"
	"testing"
)

// TestRendererIsConcurrencySafe guards the pre-render on the launch path
// (app.go starts a render as soon as a document path is known, which can
// overlap a RenderDocument call from the frontend). One *Renderer is shared by
// both, so goldmark, chroma and bluemonday all have to tolerate it. Run under
// -race; without it this test proves much less.
func TestRendererIsConcurrencySafe(t *testing.T) {
	r := New()
	src := []byte("# Title\n\n```go\nfunc main() { println(\"x\") }\n```\n\n" +
		"| a | b |\n|:-:|--:|\n| 1 | 2 |\n\n> [!NOTE]\n> alert\n\n" +
		"Math $E=mc^2$ and a footnote[^1].\n\n[^1]: note\n\n:tada: <kbd>K</kbd>\n")

	const goroutines = 16
	var wg sync.WaitGroup
	results := make([]string, goroutines)
	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			doc, err := r.Render("/tmp/x.md", src)
			if err != nil {
				t.Errorf("Render: %v", err)
				return
			}
			results[i] = doc.HTML
		}()
	}
	wg.Wait()

	// Same input through the same renderer must give byte-identical output;
	// a shared mutable cache would show up as a diff, not just as a race.
	for i, got := range results {
		if got != results[0] {
			t.Fatalf("goroutine %d produced different HTML than goroutine 0", i)
		}
		if !strings.Contains(got, "markdown-alert") || !strings.Contains(got, "chroma") {
			t.Fatalf("goroutine %d produced incomplete HTML: %q", i, got)
		}
	}
}
