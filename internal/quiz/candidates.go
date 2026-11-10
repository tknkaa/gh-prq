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
	// AI が出題したときだけ入る
	Hint        string
	Explanation string
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
	var out []Candidate
	switch level {
	case Normal:
		out = lineCandidates(file, root, t, added)
	case Hard:
		out = blockCandidates(file, l, root, src, t, added)
	default:
		out = easyCandidates(file, l, root, src, t, added)
	}
	for i := range out {
		out[i].Kind = l.KindName(out[i].Kind)
	}
	return out
}

func walk(n *sitter.Node, fn func(*sitter.Node)) {
	fn(n)
	for i := uint(0); i < n.NamedChildCount(); i++ {
		if c := n.NamedChild(i); c != nil {
			walk(c, fn)
		}
	}
}

func easyCandidates(file string, l *lang.Lang, root *sitter.Node, src []byte, t text, added map[int]bool) []Candidate {
	var out []Candidate
	walk(root, func(n *sitter.Node) {
		sr, er := int(n.StartPosition().Row), int(n.EndPosition().Row)
		if sr != er || !added[sr] || n.HasError() {
			return
		}
		kind := n.Kind()
		if l.IsHCL() {
			var ok bool
			if kind, ok = hclEasyKind(n, src); !ok {
				return
			}
		} else if l.IsYAML() {
			var ok bool
			if kind, ok = yamlEasyKind(n, src); !ok {
				return
			}
		} else if !easyKinds[kind] {
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
		out = append(out, Candidate{File: file, Start: s, End: e, StartRow: sr, EndRow: sr, Kind: kind})
	})
	return out
}

// Terraform で問うべきなのは「この引数に何を渡すか」なので、引数の右辺を丸ごと穴にする。
// 式の内部まで細かく穴にすると、参照の一部だけが空いた解きにくい問題になる。
var hclEasyKinds = map[string]bool{
	"function_call": true, "conditional": true, "operation": true, "for_expr": true,
}

func hclEasyKind(n *sitter.Node, src []byte) (string, bool) {
	if hclEasyKinds[n.Kind()] {
		return n.Kind(), true
	}
	if n.Kind() != "expression" {
		return "", false
	}
	p := n.Parent()
	if p == nil || (p.Kind() != "attribute" && p.Kind() != "object_elem") {
		return "", false
	}
	// true / false / null は二択の当て推量になるので出さない
	switch strings.TrimSpace(n.Utf8Text(src)) {
	case "true", "false", "null":
		return "", false
	}
	if c := n.NamedChild(0); c != nil {
		// 中身が関数呼び出しなどなら、その種類をヒントに使う
		if c.Kind() == "variable_expr" && n.NamedChildCount() > 1 {
			return "variable_expr", true
		}
		return c.Kind(), true
	}
	return n.Kind(), true
}

// YAML も Terraform と同じく「このキーに何を書くか」が問うべき点なので、キーの値を丸ごと穴にする。
// スカラーの一部だけを空けると、文字列の途中を当てる問題になって意味が薄い。
func yamlEasyKind(n *sitter.Node, src []byte) (string, bool) {
	if n.Kind() != "flow_node" {
		return "", false
	}
	p := n.Parent()
	if p == nil {
		return "", false
	}
	switch p.Kind() {
	case "block_mapping_pair", "flow_pair":
		// キーの側を穴にしても、何のキーかを当てる問題にしかならない
		if v := p.ChildByFieldName("value"); v == nil || v.Id() != n.Id() {
			return "", false
		}
	case "block_sequence_item":
	default:
		return "", false
	}
	// true / false / null は二択の当て推量になるので出さない
	switch strings.ToLower(strings.TrimSpace(n.Utf8Text(src))) {
	case "true", "false", "null", "yes", "no", "on", "off", "~":
		return "", false
	}
	if c := n.NamedChild(0); c != nil {
		return c.Kind(), true
	}
	return n.Kind(), true
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

func blockCandidates(file string, l *lang.Lang, root *sitter.Node, src []byte, t text, added map[int]bool) []Candidate {
	var out []Candidate
	walk(root, func(n *sitter.Node) {
		k := n.Kind()
		sr, er := int(n.StartPosition().Row), int(n.EndPosition().Row)
		var is, ie int
		kind := k
		if p := n.Parent(); p != nil {
			kind = p.Kind()
		}
		switch {
		case l.IsHCL():
			// HCL の block は `resource "x" "y" {` から始まるので、波括弧の中の body だけを穴にする
			p := n.Parent()
			if k != "body" || p == nil || p.Kind() != "block" {
				return
			}
			if strings.TrimSpace(t.line(sr)[:n.StartPosition().Column]) != "" {
				return
			}
			is, ie = sr, er
			// resource や dynamic などブロックの種類が分かると、何を書くべきかの手がかりになる
			kind = "block"
			if id := p.NamedChild(0); id != nil && id.Kind() == "identifier" {
				kind = id.Utf8Text(src) + " block"
			}
		case l.IsYAML():
			// キーの行は残して、その下にぶら下がる値のブロックだけを穴にする。キーまで消すと何を書くべきか分からない
			p := n.Parent()
			if k != "block_node" || p == nil || p.Kind() != "block_mapping_pair" {
				return
			}
			if strings.TrimSpace(t.line(sr)[:n.StartPosition().Column]) != "" {
				return
			}
			is, ie = sr, er
			// 末尾のブロックは次の行の先頭で終わることがあるので、その空の行は含めない。含めると最後の行が追加行でないとみなされる
			if n.EndPosition().Column == 0 {
				ie--
			}
			kind = "block"
			if key := p.ChildByFieldName("key"); key != nil {
				kind = "value of " + key.Utf8Text(src)
			}
		case k != "statement_block" && k != "block":
			return
		case src[n.StartByte()] == '{':
			// 波括弧の行は残して中身だけを穴にする。括弧まで消すと、どこまで書けばいいか分からなくなる
			if !strings.HasSuffix(strings.TrimRight(t.line(sr), " \t"), "{") ||
				!strings.HasPrefix(strings.TrimLeft(t.line(er), " \t"), "}") {
				return
			}
			is, ie = sr+1, er-1
		case l.IsIndentBlock():
			// Python のようにインデントでブロックを作る言語は、ブロックの行全体を穴にする
			if strings.TrimSpace(t.line(sr)[:n.StartPosition().Column]) != "" {
				return
			}
			is, ie = sr, er
		default:
			return
		}
		if ie-is+1 < 2 || ie-is+1 > 12 {
			return
		}
		for r := is; r <= ie; r++ {
			if !added[r] {
				return
			}
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
		if !conflicts(picked, c) {
			picked = append(picked, c)
			if len(picked) == n {
				break
			}
		}
	}
	return SortCandidates(picked)
}

// SortCandidates はファイルごと・位置順に並べる。Build がこの順を前提にしているため。
func SortCandidates(cs []Candidate) []Candidate {
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].File != cs[j].File {
			return cs[i].File < cs[j].File
		}
		return cs[i].Start < cs[j].Start
	})
	return cs
}
