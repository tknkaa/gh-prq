package quiz

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/tknkaa/gh-prq/internal/lang"
)

// Plan は AI が選んだ穴の一覧。AI にはコマンドを実行させず、このファイルを書かせるだけにする。
type Plan struct {
	Blanks []PlanEntry `json:"blanks"`
}

type PlanEntry struct {
	File string `json:"file"`
	// Line と EndLine は head 側のファイルの 1 始まりの行番号
	Line    int `json:"line"`
	EndLine int `json:"endLine,omitempty"`
	// Text を指定すると、Line の行にあるその文字列だけを穴にする。省略すると行全体を穴にする
	Text        string `json:"text,omitempty"`
	Hint        string `json:"hint,omitempty"`
	Explanation string `json:"explanation,omitempty"`
}

func LoadPlan(path string) (*Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("invalid plan %s: %w", path, err)
	}
	if len(p.Blanks) == 0 {
		return nil, fmt.Errorf("plan %s has no blanks", path)
	}
	return &p, nil
}

// PlanCandidate は AI の指定を穴の候補に変換する。
// AI の行番号や文字列は間違っていることがあるので、ファイルの実際の中身と突き合わせて検証する。
func PlanCandidate(l *lang.Lang, src []byte, e PlanEntry) (Candidate, error) {
	t := newText(src)
	row := e.Line - 1
	if row < 0 || row >= t.numLines() {
		return Candidate{}, fmt.Errorf("%s:%d: line out of range", e.File, e.Line)
	}
	tree := l.Parse(src)
	defer tree.Close()
	root := tree.RootNode()

	if e.Text != "" {
		if strings.Contains(e.Text, "\n") {
			return Candidate{}, fmt.Errorf("%s:%d: text must fit on one line; use line/endLine for multi-line blanks", e.File, e.Line)
		}
		ls := t.lineStart(row)
		idx := bytes.Index(src[ls:t.lineEnd(row)], []byte(e.Text))
		if idx < 0 {
			return Candidate{}, fmt.Errorf("%s:%d: text %q not found on that line", e.File, e.Line, e.Text)
		}
		start, end := ls+idx, ls+idx+len(e.Text)
		return Candidate{
			File: e.File, Start: start, End: end, StartRow: row, EndRow: row,
			Kind: exactKind(root.NamedDescendantForByteRange(uint(start), uint(end)), start, end),
			Hint: e.Hint, Explanation: e.Explanation,
		}, nil
	}

	er := row
	if e.EndLine > 0 {
		er = e.EndLine - 1
	}
	if er < row || er >= t.numLines() {
		return Candidate{}, fmt.Errorf("%s:%d-%d: invalid line range", e.File, e.Line, e.EndLine)
	}
	start, end := t.lineStart(row), t.lineEnd(er)
	first := t.line(row)
	cs := start + len(indentOf(first))
	ce := start + len(strings.TrimRight(string(src[start:end]), " \t"))
	return Candidate{
		File: e.File, Start: start, End: end, StartRow: row, EndRow: er, LineLevel: true,
		Kind: exactKind(root.NamedDescendantForByteRange(uint(cs), uint(ce)), cs, ce),
		Hint: e.Hint, Explanation: e.Explanation,
	}, nil
}

// exactKind は範囲が構文要素とぴったり一致するときだけその種類を返す。
// 一致しないのに種類を出すと、ヒントが的外れになるため。
func exactKind(n *sitter.Node, start, end int) string {
	if n != nil && int(n.StartByte()) == start && int(n.EndByte()) == end {
		return n.Kind()
	}
	return "code"
}

// FilterOverlaps は指定順に採用し、他の穴に近すぎるものを落とす。
func FilterOverlaps(cands []Candidate) (kept, dropped []Candidate) {
	for _, c := range cands {
		if conflicts(kept, c) {
			dropped = append(dropped, c)
		} else {
			kept = append(kept, c)
		}
	}
	return kept, dropped
}

func conflicts(picked []Candidate, c Candidate) bool {
	for _, p := range picked {
		if p.File == c.File && !(c.StartRow >= p.EndRow+3 || c.EndRow <= p.StartRow-3) {
			return true
		}
	}
	return false
}
