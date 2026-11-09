package quiz

import (
	"fmt"
	"slices"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

type hintToken struct {
	text  string
	row   int
	ident string
}

// makeHints は AI なしで出せるヒントを、弱いものから順に 4 つ作る。
func makeHints(root *sitter.Node, src []byte, c Candidate) []string {
	var toks []hintToken
	var rec func(n *sitter.Node)
	rec = func(n *sitter.Node) {
		s, e := int(n.StartByte()), int(n.EndByte())
		if e <= c.Start || s >= c.End {
			return
		}
		k := n.Kind()
		if strings.Contains(k, "comment") {
			return
		}
		row := int(n.StartPosition().Row)
		if s >= c.Start && e <= c.End {
			// 文字列の中身まで骨格に出すと答えそのものになるので、文字列は丸ごと伏せる
			if n.IsNamed() && isStringKind(k) {
				toks = append(toks, hintToken{text: `"_"`, row: row})
				return
			}
			if n.ChildCount() == 0 {
				t := n.Utf8Text(src)
				if strings.TrimSpace(t) == "" {
					return
				}
				switch {
				case isIdentKind(k):
					toks = append(toks, hintToken{text: "_", row: row, ident: t})
				case isLiteralKind(k):
					toks = append(toks, hintToken{text: "_", row: row})
				default:
					toks = append(toks, hintToken{text: t, row: row})
				}
				return
			}
		}
		for i := uint(0); i < n.ChildCount(); i++ {
			if ch := n.Child(i); ch != nil {
				rec(ch)
			}
		}
	}
	rec(root)

	return []string{
		kindHint(c),
		"Shape:\n" + skeleton(toks, src, c),
		usesHint(toks),
		startsWithHint(string(src[c.Start:c.End])),
	}
}

func kindHint(c Candidate) string {
	k := humanize(c.Kind)
	switch {
	case !c.LineLevel:
		return "It's " + article(k) + "."
	case c.StartRow == c.EndRow:
		return "A whole line: " + article(k) + "."
	default:
		return fmt.Sprintf("%d lines: the body of %s.", c.EndRow-c.StartRow+1, article(k))
	}
}

func skeleton(toks []hintToken, src []byte, c Candidate) string {
	t := newText(src)
	minIndent := -1
	for r := c.StartRow; r <= c.EndRow; r++ {
		if strings.TrimSpace(t.line(r)) == "" {
			continue
		}
		if n := len(indentOf(t.line(r))); minIndent < 0 || n < minIndent {
			minIndent = n
		}
	}
	var lines []string
	for r := c.StartRow; r <= c.EndRow; r++ {
		var row []string
		for _, tk := range toks {
			if tk.row == r {
				row = append(row, tk.text)
			}
		}
		if len(row) == 0 {
			continue
		}
		indent := ""
		if c.LineLevel && minIndent >= 0 {
			indent = strings.Repeat(" ", max(0, len(indentOf(t.line(r)))-minIndent))
		}
		lines = append(lines, "  "+indent+joinTokens(row))
	}
	return strings.Join(lines, "\n")
}

func usesHint(toks []hintToken) string {
	var ids []string
	for _, tk := range toks {
		if tk.ident != "" && !slices.Contains(ids, tk.ident) {
			ids = append(ids, tk.ident)
		}
	}
	if len(ids) == 0 {
		return "Uses no identifiers."
	}
	return "Uses: " + strings.Join(ids, ", ")
}

func startsWithHint(expected string) string {
	first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(expected), "\n", 2)[0])
	r := []rune(first)
	n := min(max(3, len(r)/3), 24, len(r))
	return "Starts with: " + string(r[:n]) + "…"
}

func joinTokens(toks []string) string {
	var b strings.Builder
	for i, t := range toks {
		if i > 0 {
			p := toks[i-1]
			if !noSpaceAfter(p) && !noSpaceBefore(t, p) {
				b.WriteByte(' ')
			}
		}
		b.WriteString(t)
	}
	return b.String()
}

func noSpaceAfter(p string) bool {
	switch p {
	case "(", "[", ".", "?.", "!", "...", "::":
		return true
	}
	return false
}

func noSpaceBefore(t, prev string) bool {
	switch t {
	case ")", "]", ",", ";", ".", "?.", "::":
		return true
	case "(", "[":
		// 呼び出しや添字は直前にくっつける。そうしないと `f (x)` のように読みにくい
		return prev == "_" || prev == ")" || prev == "]" || prev == ">"
	}
	return false
}

func isStringKind(k string) bool {
	switch k {
	case "string", "template_string", "interpreted_string_literal", "raw_string_literal",
		"string_literal", "concatenated_string", "char_literal", "rune_literal":
		return true
	}
	return false
}

func isIdentKind(k string) bool {
	return strings.Contains(k, "identifier") || k == "this" || k == "self"
}

func isLiteralKind(k string) bool {
	switch k {
	case "number", "integer", "float", "true", "false", "null", "none", "nil", "undefined":
		return true
	}
	return strings.HasSuffix(k, "_literal")
}

func humanize(k string) string {
	switch k {
	case "arguments", "argument_list":
		return "argument list"
	case "call":
		return "function call"
	case "attribute":
		return "attribute access"
	case "code":
		return "piece of code"
	}
	return strings.ReplaceAll(k, "_", " ")
}

func article(s string) string {
	if s != "" && strings.ContainsRune("aeiou", rune(s[0])) {
		return "an " + s
	}
	return "a " + s
}
