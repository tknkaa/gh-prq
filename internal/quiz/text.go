package quiz

import "strings"

type text struct {
	src    []byte
	starts []int
}

func newText(src []byte) text {
	starts := []int{0}
	for i, c := range src {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	return text{src: src, starts: starts}
}

func (t text) numLines() int {
	n := len(t.starts)
	// 末尾の改行の後ろは行として数えない。数えると存在しない空行がアンカーになってしまう
	if n > 1 && t.starts[n-1] == len(t.src) {
		n--
	}
	return n
}

func (t text) lineStart(r int) int { return t.starts[r] }

func (t text) lineEnd(r int) int {
	if r+1 < len(t.starts) {
		e := t.starts[r+1] - 1
		if e > t.starts[r] && t.src[e-1] == '\r' {
			e--
		}
		return e
	}
	return len(t.src)
}

func (t text) line(r int) string { return string(t.src[t.lineStart(r):t.lineEnd(r)]) }

func splitLines(s string) []string {
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

func indentOf(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}
