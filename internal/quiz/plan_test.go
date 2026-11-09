package quiz

import (
	"strings"
	"testing"

	"github.com/tknkaa/gh-prq/internal/lang"
)

func TestPlanCandidate(t *testing.T) {
	l := lang.ForFile("a.ts")
	src := []byte(tsSrc)
	c, err := PlanCandidate(l, src, PlanEntry{File: "a.ts", Line: 8, Text: "(a, i) => a + i.price", Hint: "h"})
	if err != nil || c.Kind != "arrow_function" || string(src[c.Start:c.End]) != "(a, i) => a + i.price" {
		t.Fatalf("text blank: %+v %v", c, err)
	}
	c, err = PlanCandidate(l, src, PlanEntry{File: "a.ts", Line: 11, EndLine: 12})
	if err != nil || !c.LineLevel || !strings.Contains(string(src[c.Start:c.End]), "throw") {
		t.Fatalf("line blank: %+v %v", c, err)
	}
	// AI の行番号のずれや存在しない文字列は、黙って変な穴にせずエラーにする
	for _, e := range []PlanEntry{
		{File: "a.ts", Line: 7, Text: "(a, i) => a + i.price"},
		{File: "a.ts", Line: 999},
		{File: "a.ts", Line: 11, EndLine: 10},
	} {
		if _, err := PlanCandidate(l, src, e); err == nil {
			t.Errorf("expected error for %+v", e)
		}
	}
	// AI のヒントは機械的なヒントより先に出る
	tc, _ := PlanCandidate(l, src, PlanEntry{File: "a.ts", Line: 8, Text: "(a, i) => a + i.price", Hint: "h", Explanation: "why"})
	_, blanks := Build("a.ts", l, src, []Candidate{tc}, 1)
	if len(blanks[0].Hints) != 5 || blanks[0].Hints[0] != "h" || blanks[0].Explanation != "why" {
		t.Errorf("hints: %v explanation: %q", blanks[0].Hints, blanks[0].Explanation)
	}
}
