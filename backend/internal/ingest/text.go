package ingest

import (
	"strings"

	"golang.org/x/net/html"
)

// htmlToText flattens feed HTML into plain text, keeping paragraph and list breaks.
func htmlToText(s string) string {
	var b strings.Builder
	z := html.NewTokenizer(strings.NewReader(s))
	skip := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return collapseBlankLines(b.String())
		case html.TextToken:
			if skip == 0 {
				b.Write(z.Text())
			}
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			switch tok.Data {
			case "script", "style":
				switch tok.Type { //nolint:exhaustive // only start/end tags change skip depth
				case html.StartTagToken:
					skip++
				case html.EndTagToken:
					skip = max(skip-1, 0)
				}
			case "p", "div", "br", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "pre", "blockquote":
				b.WriteString("\n")
			}
		}
	}
}

func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
			}
			blank = true
			continue
		}
		blank = false
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
