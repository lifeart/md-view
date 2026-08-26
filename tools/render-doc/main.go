// Command render-doc prints the JSON Doc that internal/render produces for a
// markdown file — the exact payload the frontend receives from RenderDocument.
// scripts/e2e-frontend.sh feeds it to the built bundle running headless.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"md-view/internal/render"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: render-doc <file.md>")
		os.Exit(2)
	}
	doc, err := render.New().RenderFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "render-doc:", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(doc); err != nil {
		fmt.Fprintln(os.Stderr, "render-doc:", err)
		os.Exit(1)
	}
}
