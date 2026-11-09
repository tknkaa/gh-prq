package quiz

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/tknkaa/gh-prq/internal/lang"
)

const tsSrc = `import { Item } from "./item";

const LIMIT = 1000;

export function total(items: Item[], tax = 0.1): number {
  const active = items.filter((i) => i.active);

  const sum = active.reduce((a, i) => a + i.price, 0);

  if (sum > LIMIT) {
    console.warn("too much", sum);
    throw new Error("limit exceeded");
  }

  return Math.round(sum * (1 + tax));
}
`

func allRows(src string) map[int]bool {
	m := map[int]bool{}
	for i := range strings.Count(src, "\n") {
		m[i] = true
	}
	return m
}

// 全レベルで「正解を書けば正解、間違えれば不正解、空白違いは正解」になることを確かめる
func TestRoundTrip(t *testing.T) {
	l := lang.ForFile("a.ts")
	orig := []byte(tsSrc)
	for _, lv := range []Level{Easy, Normal, Hard} {
		t.Run(string(lv), func(t *testing.T) {
			cands := FindCandidates("a.ts", l, orig, allRows(tsSrc), lv)
			if len(cands) == 0 {
				t.Fatal("no candidates")
			}
			picked := Select(cands, 10, rand.New(rand.NewPCG(1, 2)))
			blanked, blanks := Build("a.ts", l, orig, picked, 1)
			for i := range blanks {
				b := &blanks[i]
				ex := Extract(blanked, b)
				if !ex.Found || ex.Answered {
					t.Fatalf("#%d before answer: %+v\n%s", b.ID, ex, blanked)
				}
				fill := func(ans string) []byte {
					rep := ans
					if b.LineLevel {
						rep = ans
						return []byte(replaceLine(string(blanked), b.Placeholder, rep))
					}
					return []byte(strings.Replace(string(blanked), b.Placeholder, rep, 1))
				}
				for _, tc := range []struct {
					ans  string
					want bool
				}{
					{b.Expected, true},
					{spaced(b.Expected), true},
					{b.Expected + " + 1", false},
				} {
					ex := Extract(fill(tc.ans), b)
					if !ex.Found || !ex.Answered {
						t.Fatalf("#%d %q not extracted: %+v", b.ID, tc.ans, ex)
					}
					if got := Grade(l, orig, b, ex.Answer); got != tc.want {
						t.Errorf("#%d (%s) answer %q: got %v want %v", b.ID, b.Kind, ex.Answer, got, tc.want)
					}
				}
				if len(b.Hints) != 4 {
					t.Errorf("hints: %v", b.Hints)
				}
			}
		})
	}
}

func replaceLine(s, placeholder, rep string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.Contains(l, placeholder) {
			lines[i] = rep
		}
	}
	return strings.Join(lines, "\n")
}

func spaced(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "(", "( "), ",", " ,")
}

func TestHints(t *testing.T) {
	l := lang.ForFile("a.ts")
	orig := []byte(tsSrc)
	start := strings.Index(tsSrc, "(a, i) => a + i.price")
	c := Candidate{Start: start, End: start + len("(a, i) => a + i.price"), StartRow: 7, EndRow: 7, Kind: "arrow_function"}
	tree := l.Parse(orig)
	defer tree.Close()
	h := makeHints(tree.RootNode(), orig, c)
	want := []string{
		"It's an arrow function.",
		"Shape:\n  (_, _) => _ + _._",
		"Uses: a, i, price",
		"Starts with: (a, i) …",
	}
	for i := range want {
		if h[i] != want[i] {
			t.Errorf("hint %d: got %q want %q", i+1, h[i], want[i])
		}
	}
}
