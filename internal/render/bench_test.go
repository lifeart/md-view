package render

import (
	"bytes"
	"fmt"
	"testing"
)

// buildProse returns roughly size bytes of markdown prose (headings,
// paragraphs, lists, links) — the typical-document benchmark input.
func buildProse(size int) []byte {
	var b bytes.Buffer
	for i := 0; b.Len() < size; i++ {
		fmt.Fprintf(&b, "## Section %d\n\n", i)
		for j := 0; j < 4; j++ {
			fmt.Fprintf(&b, "Paragraph %d.%d with some *emphasis*, **strong** text, `inline code`, "+
				"and a [link](https://example.com/%d). The quick brown fox jumps over the lazy dog "+
				"while the renderer keeps its budget in check.\n\n", i, j, j)
		}
		b.WriteString("- item one\n- item two\n- item three\n\n")
	}
	return b.Bytes()
}

// buildCodeFences returns markdown that is almost entirely fenced Go code:
// count fences of roughly fenceSize bytes each.
func buildCodeFences(count, fenceSize int) []byte {
	var b bytes.Buffer
	for i := 0; i < count; i++ {
		fmt.Fprintf(&b, "```go\n")
		var f bytes.Buffer
		for j := 0; f.Len() < fenceSize; j++ {
			fmt.Fprintf(&f, "func handler%d_%d(w http.ResponseWriter, r *http.Request) error {\n"+
				"\tif r.Method != http.MethodGet { // %d\n"+
				"\t\treturn fmt.Errorf(\"method %%s not allowed\", r.Method)\n"+
				"\t}\n"+
				"\treturn json.NewEncoder(w).Encode(map[string]int{\"n\": %d})\n"+
				"}\n\n", i, j, j, j)
		}
		b.Write(f.Bytes())
		b.WriteString("```\n\n")
	}
	return b.Bytes()
}

const megabyte = 1 << 20

// BenchmarkRender1MBProse: the typical-document case from the performance
// budget in ARCHITECTURE.md (1 MB parse+render < 400 ms, prose ~150 ms).
func BenchmarkRender1MBProse(b *testing.B) {
	r := New()
	src := buildProse(megabyte)
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Render("/bench/prose.md", src); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRender1MBSingleCodeFence: the pathological chroma-bound case — one
// giant fence. Fences over the highlight cap must render as plain escaped
// code, keeping this bounded.
func BenchmarkRender1MBSingleCodeFence(b *testing.B) {
	r := New()
	src := buildCodeFences(1, megabyte)
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Render("/bench/code.md", src); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRender1MBManyCodeFences: 1 MB split into 64 KB fences (each above
// the highlight cap).
func BenchmarkRender1MBManyCodeFences(b *testing.B) {
	r := New()
	src := buildCodeFences(16, 64<<10)
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Render("/bench/code-many.md", src); err != nil {
			b.Fatal(err)
		}
	}
}
