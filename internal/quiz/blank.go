package quiz

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/tknkaa/gh-prq/internal/lang"
)

type Blank struct {
	ID          int      `json:"id"`
	File        string   `json:"file"`
	Kind        string   `json:"kind"`
	LineLevel   bool     `json:"lineLevel"`
	Start       int      `json:"start"`
	End         int      `json:"end"`
	Expected    string   `json:"expected"`
	Prefix      string   `json:"prefix"`
	Suffix      string   `json:"suffix"`
	Placeholder string   `json:"placeholder"`
	AnchorPrev  []string `json:"anchorPrev"`
	AnchorNext  []string `json:"anchorNext"`
	Line        int      `json:"line"`
	Hints       []string `json:"hints"`
	Explanation string   `json:"explanation,omitempty"`

	Answer     string `json:"answer"`
	Answered   bool   `json:"answered"`
	Correct    bool   `json:"correct"`
	Attempts   int    `json:"attempts"`
	HintsShown int    `json:"hintsShown"`
	Revealed   bool   `json:"revealed"`
	Marked     bool   `json:"marked"`
	Missing    bool   `json:"missing"`
}

func (b *Blank) Resolved() bool { return b.Correct || b.Revealed || b.Marked }

func (b *Blank) Outcome() string {
	switch {
	case b.Marked:
		return "marked"
	case b.Revealed:
		return "revealed"
	case b.Correct && b.HintsShown > 0:
		return "solved_with_hints"
	case b.Correct:
		return "solved"
	case b.Missing:
		return "missing"
	case b.Answered:
		return "incorrect"
	}
	return "unanswered"
}

// Build は穴を空けたファイルの中身と、穴の情報を返す。cands は同じファイルのもので、位置順に並んでいること。
func Build(file string, l *lang.Lang, orig []byte, cands []Candidate, firstID int) ([]byte, []Blank) {
	tree := l.Parse(orig)
	defer tree.Close()
	t := newText(orig)
	blanks := make([]Blank, len(cands))
	reps := make([]string, len(cands))
	for i, c := range cands {
		id := firstID + i
		b := Blank{
			ID: id, File: file, Kind: c.Kind, LineLevel: c.LineLevel,
			Start: c.Start, End: c.End, Expected: string(orig[c.Start:c.End]),
		}
		if c.LineLevel {
			// 番号の後ろに ":" を付けるのは、#1 が #10 の前方一致で誤検出されないようにするため
			b.Placeholder = fmt.Sprintf("%s prq#%d: fill in", l.Comment, id)
			reps[i] = indentOf(t.line(c.StartRow)) + b.Placeholder
		} else {
			b.Prefix = string(orig[t.lineStart(c.StartRow):c.Start])
			b.Suffix = string(orig[c.End:t.lineEnd(c.EndRow)])
			b.Placeholder = fmt.Sprintf("__prq_%d__", id)
			reps[i] = b.Placeholder
		}
		for r := c.StartRow - 2; r < c.StartRow; r++ {
			if r >= 0 {
				b.AnchorPrev = append(b.AnchorPrev, t.line(r))
			}
		}
		for r := c.EndRow + 1; r <= c.EndRow+2 && r < t.numLines(); r++ {
			b.AnchorNext = append(b.AnchorNext, t.line(r))
		}
		b.Hints = makeHints(tree.RootNode(), orig, c)
		// AI のヒントは意図に関するもので機械的なヒントより弱いので、先に出す
		if c.Hint != "" {
			b.Hints = append([]string{c.Hint}, b.Hints...)
		}
		b.Explanation = c.Explanation
		blanks[i] = b
	}
	out := slices.Clone(orig)
	// 後ろから置き換えると、前の穴のバイト位置がずれない
	for i := len(cands) - 1; i >= 0; i-- {
		out = slices.Concat(out[:cands[i].Start], []byte(reps[i]), out[cands[i].End:])
	}
	for i := range blanks {
		idx := bytes.Index(out, []byte(blanks[i].Placeholder))
		blanks[i].Line = bytes.Count(out[:idx], []byte("\n")) + 1
	}
	return out, blanks
}

type Extraction struct {
	Found    bool
	Answered bool
	Answer   string
	// 今のファイルで回答が書かれている行の範囲 [FromRow, ToRow)
	FromRow int
	ToRow   int
}

// Extract は編集後のファイルから回答を取り出す。
// 行番号はユーザーの編集でずれるので、穴の前後の行をアンカーにして位置を探す。
func Extract(cur []byte, b *Blank) Extraction {
	lines := splitLines(string(cur))
	norm := make([]string, len(lines))
	for i, l := range lines {
		norm[i] = strings.TrimRight(l, " \t")
	}
	match := func(at int, anchor []string) bool {
		if at < 0 || at+len(anchor) > len(norm) {
			return false
		}
		for k, a := range anchor {
			if norm[at+k] != strings.TrimRight(a, " \t") {
				return false
			}
		}
		return true
	}
	start := 0
	if len(b.AnchorPrev) > 0 {
		want := b.Line - 1 - len(b.AnchorPrev)
		best := -1
		for i := 0; i+len(b.AnchorPrev) <= len(norm); i++ {
			if match(i, b.AnchorPrev) && (best < 0 || abs(i-want) < abs(best-want)) {
				best = i
			}
		}
		if best < 0 {
			return Extraction{}
		}
		start = best + len(b.AnchorPrev)
	}
	end := len(norm)
	if len(b.AnchorNext) > 0 {
		end = -1
		for j := start; j+len(b.AnchorNext) <= len(norm); j++ {
			if match(j, b.AnchorNext) {
				end = j
				break
			}
		}
		if end < 0 {
			return Extraction{}
		}
	}
	ex := Extraction{Found: true, FromRow: start, ToRow: end}
	if b.LineLevel {
		// プレースホルダーのコメントを消し忘れても、その下に書いた内容を回答として扱う
		var kept []string
		for _, l := range lines[start:end] {
			if !strings.Contains(l, b.Placeholder) {
				kept = append(kept, l)
			}
		}
		region := strings.Join(kept, "\n")
		if strings.TrimSpace(region) == "" {
			return ex
		}
		ex.Answered, ex.Answer = true, region
		return ex
	}
	r := strings.TrimRight(strings.Join(lines[start:end], "\n"), " \t\n")
	sfx := strings.TrimRight(b.Suffix, " \t")
	if len(r) < len(b.Prefix)+len(sfx) || !strings.HasPrefix(r, b.Prefix) || !strings.HasSuffix(r, sfx) {
		return Extraction{}
	}
	ans := r[len(b.Prefix) : len(r)-len(sfx)]
	if strings.TrimSpace(ans) == "" || strings.Contains(ans, b.Placeholder) {
		return ex
	}
	ex.Answered, ex.Answer = true, ans
	return ex
}

// Grade は回答を元のファイルに埋め戻し、構文木の葉の並びで比べる。
// 文字列で比べると空白や改行の違いまで不正解になって理不尽なので。
func Grade(l *lang.Lang, orig []byte, b *Blank, answer string) bool {
	cand := slices.Concat(orig[:b.Start], []byte(answer), orig[b.End:])
	return slices.Equal(Tokens(l, orig), Tokens(l, cand))
}

func Tokens(l *lang.Lang, src []byte) []string {
	tree := l.Parse(src)
	defer tree.Close()
	var out []string
	var rec func(n *sitter.Node)
	rec = func(n *sitter.Node) {
		if strings.Contains(n.Kind(), "comment") {
			return
		}
		if n.ChildCount() == 0 {
			if t := n.Utf8Text(src); strings.TrimSpace(t) != "" {
				out = append(out, t)
			}
			return
		}
		for i := uint(0); i < n.ChildCount(); i++ {
			if c := n.Child(i); c != nil {
				rec(c)
			}
		}
	}
	rec(tree.RootNode())
	// 末尾カンマの有無はフォーマッタの好みでしかないので、閉じ括弧の直前のカンマは無視する
	var res []string
	for i, t := range out {
		if t == "," && i+1 < len(out) && (out[i+1] == ")" || out[i+1] == "]" || out[i+1] == "}") {
			continue
		}
		res = append(res, t)
	}
	return res
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
