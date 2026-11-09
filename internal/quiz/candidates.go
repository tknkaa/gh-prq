package quiz

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/tknkaa/gh-prq/internal/lang"
)

type Level string

const (
	Easy   Level = "easy"
	Normal Level = "normal"
	Hard   Level = "hard"
)

func ParseLevel(s string) (Level, error) {
	switch Level(s) {
	case Easy, Normal, Hard:
		return Level(s), nil
	}
	return "", fmt.Errorf("unknown level %q (easy, normal, hard)", s)
}

// Candidate は穴にできる範囲。行単位の穴は行頭から行末までを含む。
type Candidate struct {
	File      string
	Start     int
	End       int
	StartRow  int
	EndRow    int
	Kind      string
	LineLevel bool
}

// 小さすぎる識別子やリテラルは当て推量になるので、意味のまとまりがある式だけを穴にする
var easyKinds = map[string]bool{
	"call_expression": true, "arguments": true, "argument_list": true,
	"binary_expression": true, "unary_expression": true, "arrow_function": true,
	"member_expression": true, "parenthesized_expression": true,
	"ternary_expression": true, "conditional_expression": true,
	"template_string": true, "new_expression": true, "await_expression": true,
	"selector_expression": true, "composite_literal": true, "index_expression": true,
	"slice_expression": true, "field_expression": true, "macro_invocation": true,
	"closure_expression": true, "reference_expression": true, "try_expression": true,
	"call": true, "attribute": true, "comparison_operator": true,
	"boolean_operator": true, "lambda": true, "subscript": true,
	"list_comprehension": true,
}

func FindCandidates(file string, l *lang.Lang, src []byte, added map[int]bool, level Level) []Candidate {
	tree := l.Parse(src)
	defer tree.Close()
	t := newText(src)
	root := tree.RootNode()
	switch level {
	case Normal:
		return lineCandidates(file, root, t, added)
	case Hard:
		return blockCandidates(file, root, src, t, added)
	default:
		return easyCandidates(file, root, src, t, added)
	}
}

func walk(n *sitter.Node, fn func(*sitter.Node)) {
	fn(n)
	for i := uint(0); i < n.NamedChildCount(); i++ {
		if c := n.NamedChild(i); c != nil {
			walk(c, fn)
		}
	}
}

func easyCandidates(file string, root *sitter.Node, src []byte, t text, added map[int]bool) []Candidate {
	var out []Candidate
	walk(root, func(n *sitter.Node) {
		sr, er := int(n.StartPosition().Row), int(n.EndPosition().Row)
		if sr != er || !added[sr] || !easyKinds[n.Kind()] || n.HasError() {
			return
		}
		s, e := int(n.StartByte()), int(n.EndByte())
		txt := string(src[s:e])
		if len(txt) < 3 || len(txt) > 80 {
			return
		}
		// 行全体と同じなら normal の穴なので、easy では出さない
		if strings.TrimSpace(t.line(sr)) == strings.TrimSpace(txt) {
			return
		}
		out = append(out, Candidate{File: file, Start: s, End: e, StartRow: sr, EndRow: sr, Kind: n.Kind()})
	})
	return out
}

func lineCandidates(file string, root *sitter.Node, t text, added map[int]bool) []Candidate {
	var out []Candidate
	for _, row := range sortedRows(added) {
		if row >= t.numLines() {
			continue
		}
		line := t.line(row)
		if len(strings.TrimSpace(line)) < 6 {
			continue
		}
		start := sitter.Point{Row: uint(row), Column: uint(len(indentOf(line)))}
		end := sitter.Point{Row: uint(row), Column: uint(len(strings.TrimRight(line, " \t")))}
		n := root.NamedDescendantForPointRange(start, end)
		// 行が 1 つの構文要素とぴったり一致するときだけ穴にする。`foo(` のような途中の行は答えが一意に決まらない
		if n == nil || n.StartPosition() != start || n.EndPosition() != end {
			continue
		}
		if strings.Contains(n.Kind(), "comment") || n.IsError() || n.HasError() {
			continue
		}
		out = append(out, Candidate{
			File: file, Start: t.lineStart(row), End: t.lineEnd(row),
			StartRow: row, EndRow: row, Kind: n.Kind(), LineLevel: true,
		})
	}
	return out
}

func blockCandidates(file string, root *sitter.Node, src []byte, t text, added map[int]bool) []Candidate {
	var out []Candidate
	walk(root, func(n *sitter.Node) {
		k := n.Kind()
		if k != "statement_block" && k != "block" {
			return
		}
		sr, er := int(n.StartPosition().Row), int(n.EndPosition().Row)
		var is, ie int
		if src[n.StartByte()] == '{' {
			// 波括弧の行は残して中身だけを穴にする。括弧まで消すと、どこまで書けばいいか分からなくなる
			if !strings.HasSuffix(strings.TrimRight(t.line(sr), " \t"), "{") ||
				!strings.HasPrefix(strings.TrimLeft(t.line(er), " \t"), "}") {
				return
			}
			is, ie = sr+1, er-1
		} else {
			// Python のようにインデントでブロックを作る言語は、ブロックの行全体を穴にする
			if strings.TrimSpace(t.line(sr)[:n.StartPosition().Column]) != "" {
				return
			}
			is, ie = sr, er
		}
		if ie-is+1 < 2 || ie-is+1 > 12 {
			return
		}
		for r := is; r <= ie; r++ {
			if !added[r] {
				return
			}
		}
		kind := k
		if p := n.Parent(); p != nil {
			kind = p.Kind()
		}
		out = append(out, Candidate{
			File: file, Start: t.lineStart(is), End: t.lineEnd(ie),
			StartRow: is, EndRow: ie, Kind: kind, LineLevel: true,
		})
	})
	return out
}

func sortedRows(m map[int]bool) []int {
	rows := make([]int, 0, len(m))
	for r := range m {
		rows = append(rows, r)
	}
	sort.Ints(rows)
	return rows
}

// Select は穴どうしの間に 2 行以上の手付かずの行を残す。
// 回答の位置は前後 2 行をアンカーにして探すので、アンカーが他の穴と重なると見失うため。
func Select(cands []Candidate, n int, rng *rand.Rand) []Candidate {
	var picked []Candidate
	for _, i := range rng.Perm(len(cands)) {
		c := cands[i]
		ok := true
		for _, p := range picked {
			if p.File == c.File && !(c.StartRow >= p.EndRow+3 || c.EndRow <= p.StartRow-3) {
				ok = false
				break
			}
		}
		if ok {
			picked = append(picked, c)
			if len(picked) == n {
				break
			}
		}
	}
	sort.Slice(picked, func(i, j int) bool {
		if picked[i].File != picked[j].File {
			return picked[i].File < picked[j].File
		}
		return picked[i].Start < picked[j].Start
	})
	return picked
}
