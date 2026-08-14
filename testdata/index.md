# md-view Test Document

A linked document set exercising GFM rendering, navigation and sanitization.

## Formatting

Some **bold**, *italic*, ~~strikethrough~~ and `inline code`.

> A blockquote with a [link to the sub page](sub/page.md) inside it.

## Code Samples

```go
package main

import "fmt"

func main() {
	fmt.Println("hello from md-view")
}
```

```javascript
const greet = (name) => {
  console.log(`hello, ${name}`);
};
greet("md-view");
```

## A Table

| Feature | Status | Notes |
|---------|--------|-------|
| GFM tables | done | with alignment |
| Task lists | done | see below |
| Highlighting | done | chroma, class-based |

## Tasks

- [x] Render GFM
- [x] Highlight code
- [ ] Ship M3 polish

## Media and Links

A local image:

![one blue pixel](pixel.png)

- In-document anchor: [jump to Code Samples](#code-samples)
- Relative markdown link: [sub page](sub/page.md)
- Markdown link with fragment: [sub page, details section](sub/page.md#details)
- External link: [Wails documentation](https://wails.io/docs/introduction)
- Non-markdown local file: [plain text notes](notes.txt)

---

## Some Heading

The sub page links back to this exact heading.
