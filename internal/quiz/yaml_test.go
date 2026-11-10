package quiz

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/tknkaa/gh-prq/internal/lang"
)

const yamlSrc = `name: CI
on:
  push:
    branches: [main]
jobs:
  test:
    runs-on: ubuntu-latest
    if: github.event_name == 'push'
    steps:
      - uses: actions/checkout@v4
      - run: go test ./...
        env:
          FOO: "bar ${{ secrets.X }}"
          DEBUG: true
`

func TestYAMLCandidates(t *testing.T) {
	l := lang.ForFile("ci.yml")
	if l == nil || !l.IsYAML() {
		t.Fatal(".yml is not YAML")
	}
	src := []byte(yamlSrc)
	easy := FindCandidates("ci.yml", l, src, allRows(yamlSrc), Easy)
	var texts []string
	for _, c := range easy {
		texts = append(texts, c.Kind+": "+string(src[c.Start:c.End]))
	}
	got := strings.Join(texts, "\n")
	for _, want := range []string{
		"value: ubuntu-latest",
		"value: github.event_name == 'push'",
		"list: [main]",
		"quoted string: \"bar ${{ secrets.X }}\"",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing easy candidate %q in\n%s", want, got)
		}
	}
	// キーや true は穴にしない
	for _, bad := range []string{": runs-on", ": true"} {
		if strings.Contains(got+"\n", bad+"\n") {
			t.Errorf("unexpected candidate %q in\n%s", bad, got)
		}
	}

	hard := FindCandidates("ci.yml", l, src, allRows(yamlSrc), Hard)
	var kinds []string
	for _, c := range hard {
		kinds = append(kinds, c.Kind)
		if strings.HasPrefix(string(src[c.Start:c.End]), "  steps:") {
			t.Errorf("hard blank includes its key: %q", src[c.Start:c.End])
		}
	}
	if !strings.Contains(strings.Join(kinds, ","), "value of steps") {
		t.Errorf("hard kinds: %v", kinds)
	}
}

func TestYAMLRoundTrip(t *testing.T) {
	l := lang.ForFile("ci.yaml")
	src := []byte(yamlSrc)
	for _, lv := range []Level{Easy, Normal, Hard} {
		cands := FindCandidates("ci.yaml", l, src, allRows(yamlSrc), lv)
		if len(cands) == 0 {
			t.Fatalf("%s: no candidates", lv)
		}
		picked := Select(cands, 10, rand.New(rand.NewPCG(3, 4)))
		blanked, blanks := Build("ci.yaml", l, src, picked, 1)
		for i := range blanks {
			b := &blanks[i]
			var filled string
			if b.LineLevel {
				filled = replaceLine(string(blanked), b.Placeholder, b.Expected)
			} else {
				filled = strings.Replace(string(blanked), b.Placeholder, b.Expected, 1)
			}
			ex := Extract([]byte(filled), b)
			if !ex.Answered || !Grade(l, src, b, ex.Answer) {
				t.Errorf("%s #%d (%s): correct answer not accepted: %+v", lv, b.ID, b.Kind, ex)
			}
			wrong := strings.Replace(filled, strings.TrimSpace(b.Expected), "wrong", 1)
			if ex := Extract([]byte(wrong), b); ex.Answered && Grade(l, src, b, ex.Answer) {
				t.Errorf("%s #%d: wrong answer accepted", lv, b.ID)
			}
		}
	}
}
