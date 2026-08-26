package render

import (
	"bytes"
	"html"
	"strings"
)

// A document that opens with a `---` fence encloses YAML front matter (Jekyll,
// Hugo, Obsidian, and most AI chat exports write it). Plain CommonMark has no
// idea: goldmark turns the opening fence into an <hr> and the YAML body into a
// setext heading, so the document starts with a rule and a giant mangled title.
//
// GitHub renders front matter as a two-column table above the document, and
// that is what splitFrontMatter + frontMatterTable reproduce. The parse is
// deliberately shallow — top-level `key: value` pairs — because that is what
// the table can show; anything it cannot read falls back to showing the block
// verbatim rather than guessing.

// splitFrontMatter returns the YAML body and the remaining markdown. When src
// has no front matter it returns (nil, src).
func splitFrontMatter(src []byte) (yaml, rest []byte) {
	body := src
	if after, ok := bytes.CutPrefix(body, []byte("\xef\xbb\xbf")); ok { // UTF-8 BOM
		body = after
	}
	if !bytes.HasPrefix(body, []byte("---\n")) && !bytes.HasPrefix(body, []byte("---\r\n")) {
		return nil, src
	}
	_, after, _ := bytes.Cut(body, []byte("\n"))
	for offset := 0; offset < len(after); {
		line := after[offset:]
		if i := bytes.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}
		if t := strings.TrimRight(string(line), "\r"); t == "---" || t == "..." {
			return after[:offset], after[offset+len(line)+1:]
		}
		if len(line) == len(after[offset:]) { // last line, unterminated
			break
		}
		offset += len(line) + 1
	}
	// No closing fence: this was not front matter after all.
	return nil, src
}

// frontMatterTable renders the YAML as GitHub does: one row per top-level key.
func frontMatterTable(yaml []byte) string {
	rows, ok := parseFrontMatter(yaml)
	if !ok {
		return `<pre class="frontmatter"><code>` + html.EscapeString(string(yaml)) + "</code></pre>\n"
	}
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<table class="frontmatter"><tbody>`)
	for _, r := range rows {
		b.WriteString("<tr><th>")
		b.WriteString(html.EscapeString(r.key))
		b.WriteString("</th><td>")
		b.WriteString(html.EscapeString(r.value))
		b.WriteString("</td></tr>")
	}
	b.WriteString("</tbody></table>\n")
	return b.String()
}

type frontMatterRow struct{ key, value string }

// parseFrontMatter reads top-level `key: value` pairs. A key whose value is on
// following indented lines collapses to a comma-joined summary — enough for the
// tag lists and author arrays that make up nearly all real front matter. ok is
// false when a line is neither a comment, a blank, a top-level key, nor part of
// a nested value, in which case the caller shows the raw block instead.
func parseFrontMatter(yaml []byte) ([]frontMatterRow, bool) {
	var rows []frontMatterRow
	var nested []string
	flush := func() {
		if len(nested) > 0 && len(rows) > 0 {
			rows[len(rows)-1].value = strings.Join(nested, ", ")
		}
		nested = nil
	}
	for _, raw := range strings.Split(string(yaml), "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' || strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
			if len(rows) == 0 {
				return nil, false
			}
			nested = append(nested, cleanScalar(strings.TrimPrefix(trimmed, "- ")))
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(key) == "" {
			return nil, false
		}
		flush()
		rows = append(rows, frontMatterRow{
			key:   strings.TrimSpace(key),
			value: cleanScalar(value),
		})
	}
	flush()
	return rows, true
}

// cleanScalar strips YAML quoting and flow-sequence brackets so the table shows
// `a, b` rather than `[a, b]` or `"a"`.
func cleanScalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '[' && s[len(s)-1] == ']' || s[0] == '{' && s[len(s)-1] == '}') {
		parts := strings.Split(s[1:len(s)-1], ",")
		for i := range parts {
			parts[i] = cleanScalar(parts[i])
		}
		return strings.Join(parts, ", ")
	}
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}
