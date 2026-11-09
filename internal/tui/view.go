package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tknkaa/gh-prq/internal/quiz"
)

// GitHub の Primer に寄せた配色
var (
	green  = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	red    = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}
	yellow = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"}
	blue   = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
	purple = lipgloss.AdaptiveColor{Light: "#8250df", Dark: "#a371f7"}
	muted  = lipgloss.AdaptiveColor{Light: "#656d76", Dark: "#8d96a0"}
	border = lipgloss.AdaptiveColor{Light: "#d0d7de", Dark: "#3d444d"}
	addBg  = lipgloss.AdaptiveColor{Light: "#dafbe1", Dark: "#1b3a28"}
	selBg  = lipgloss.AdaptiveColor{Light: "#ddf4ff", Dark: "#1c2b41"}

	bold    = lipgloss.NewStyle().Bold(true)
	dim     = lipgloss.NewStyle().Foreground(muted)
	sGreen  = lipgloss.NewStyle().Foreground(green)
	sRed    = lipgloss.NewStyle().Foreground(red)
	sYellow = lipgloss.NewStyle().Foreground(yellow)
	sBlue   = lipgloss.NewStyle().Foreground(blue)
	sPurple = lipgloss.NewStyle().Foreground(purple)
	sBorder = lipgloss.NewStyle().Foreground(border)
	sKey    = lipgloss.NewStyle().Foreground(blue).Bold(true)
	sBanner = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(green).Bold(true).Padding(0, 1)
)

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const (
	// これより狭い端末では 2 列にすると両方とも読めなくなるので、縦に積む
	twoColumnMin = 90
	listWidthMax = 34
)

func (m *Model) View() string {
	if m.s == nil {
		return m.loadingView()
	}
	return m.mainView()
}

func (m *Model) size() (int, int) {
	w, h := m.width, m.height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 40
	}
	return w, h
}

func (m *Model) loadingView() string {
	w, _ := m.size()
	var b strings.Builder
	b.WriteString("\n " + bold.Render("prq") + "\n\n")
	for i, name := range m.stepNames {
		var mark string
		switch {
		case m.err != nil && i == m.step:
			mark = sRed.Render("✗")
		case i < m.step:
			mark = sGreen.Render("✓")
		case i == m.step:
			mark = sBlue.Render(spinner[m.frame%len(spinner)])
		default:
			mark = dim.Render("·")
		}
		line := " " + mark + " " + name
		if d := m.details[i]; d != "" && i <= m.step {
			line += "  " + dim.Render(shortPath(d))
		}
		b.WriteString(ansi.Wrap(line, w-1, "") + "\n")
	}
	if m.err != nil {
		b.WriteString("\n" + indent(ansi.Wrap(sRed.Render(m.err.Error()), w-2, " /"), 1) + "\n\n " + dim.Render("Press q to quit.") + "\n")
	}
	return b.String()
}

func (m *Model) mainView() string {
	s := m.s
	w, h := m.size()

	header := m.headerView(w)
	footer := m.footerView(w)
	bodyH := h - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyH < 6 {
		bodyH = 6
	}

	var body string
	if w >= twoColumnMin {
		lw := min(listWidthMax, w/3)
		rw := w - lw
		left := m.listBox(lw, bodyH)
		right := clip(m.detailView(&s.Blanks[m.cursor], rw), bodyH)
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	} else {
		list := m.listBox(w, min(len(s.Blanks)+len(s.Files())+2, bodyH/3))
		detail := clip(m.detailView(&s.Blanks[m.cursor], w), bodyH-lipgloss.Height(list))
		body = lipgloss.JoinVertical(lipgloss.Left, list, detail)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m *Model) headerView(w int) string {
	s := m.s
	resolved := 0
	for i := range s.Blanks {
		if s.Blanks[i].Resolved() {
			resolved++
		}
	}
	progress := fmt.Sprintf("%d/%d resolved", resolved, len(s.Blanks))
	if resolved == len(s.Blanks) {
		progress = sGreen.Render(progress)
	} else {
		progress = dim.Render(progress)
	}
	title := bold.Render(s.Target.Title)
	if s.Target.PR > 0 {
		title += " " + dim.Render(fmt.Sprintf("#%d", s.Target.PR))
	}
	title = ansi.Truncate(title, w-lipgloss.Width(progress)-3, "…")
	gap := max(1, w-2-lipgloss.Width(title)-lipgloss.Width(progress))
	lines := []string{" " + title + strings.Repeat(" ", gap) + progress}

	meta := fmt.Sprintf("%s → %s · %s · %s", s.Target.HeadRef, s.Target.BaseRef, s.Level, shortPath(s.Worktree))
	lines = append(lines, indent(dim.Render(ansi.Wrap(meta, w-2, " /")), 1))
	for _, n := range s.Notes {
		lines = append(lines, indent(sYellow.Render(ansi.Wrap("! "+n, w-2, " ")), 1))
	}
	if s.AllResolved() {
		lines = append(lines, " "+sBanner.Render("All blanks resolved")+"  "+dim.Render("Press q to finish."))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) footerView(w int) string {
	keys := [][2]string{
		{"j/k", "move"}, {"h", "hint"}, {"a", "answer"}, {"m", "mark correct"}, {"y", "copy path"}, {"q", "quit"},
	}
	var parts []string
	for _, k := range keys {
		parts = append(parts, sKey.Render(k[0])+" "+dim.Render(k[1]))
	}
	line := " " + strings.Join(parts, "   ")
	if m.flash != "" {
		return indent(sYellow.Render(ansi.Wrap(m.flash, w-2, " /")), 1) + "\n" + ansi.Truncate(line, w, "")
	}
	return ansi.Truncate(line, w, "")
}

func (m *Model) listBox(w, h int) string {
	inner := w - 4
	var lines []string
	lastFile := ""
	for i := range m.s.Blanks {
		b := &m.s.Blanks[i]
		if b.File != lastFile {
			if lastFile != "" {
				lines = append(lines, "")
			}
			// 深いパスでもファイル名が見えるよう、左側を削る
			lines = append(lines, bold.Render(ansi.TruncateLeft(b.File, max(0, len(b.File)-inner), "…")))
			lastFile = b.File
		}
		mark, label := outcomeMark(b)
		cursor := "  "
		if i == m.cursor {
			cursor = sBlue.Render("▶ ")
		}
		row := fmt.Sprintf("%s%s #%-2d %s  %s", cursor, mark, b.ID, dim.Render(fmt.Sprintf(":%-4d", m.currentLine(b))), label)
		row = ansi.Truncate(row, inner, "…")
		if i == m.cursor {
			row = lipgloss.NewStyle().Background(selBg).Width(inner).Render(row)
		}
		lines = append(lines, row)
	}
	return box("Blanks", "", strings.Join(lines, "\n"), w, h)
}

func outcomeMark(b *quiz.Blank) (string, string) {
	switch b.Outcome() {
	case "solved":
		return sGreen.Render("✓"), sGreen.Render("Solved")
	case "solved_with_hints":
		return sGreen.Render("✓"), sGreen.Render("Solved") + dim.Render(" w/ hints")
	case "marked":
		return sPurple.Render("✓"), sPurple.Render("Marked")
	case "revealed":
		return sBlue.Render("◉"), sBlue.Render("Revealed")
	case "incorrect":
		return sRed.Render("✗"), sRed.Render("Incorrect")
	case "missing":
		return sYellow.Render("?"), sYellow.Render("Not found")
	}
	return dim.Render("·"), dim.Render("Open")
}

func (m *Model) detailView(b *quiz.Blank, w int) string {
	inner := w - 4
	_, label := outcomeMark(b)
	title := fmt.Sprintf("#%d  %s:%d", b.ID, b.File, m.currentLine(b))
	code := m.codeLines(b, inner)
	if b.Missing {
		code += "\n\n" + sYellow.Render(ansi.Wrap("The lines around this blank were changed, so the answer can't be located. Restore them to continue.", inner, " "))
	}
	boxes := []string{box(title, label, code, w, 0)}

	if b.Answered && !b.Resolved() {
		t := "Your answer"
		if b.Attempts > 0 {
			t += fmt.Sprintf(" · %d %s", b.Attempts, plural(b.Attempts, "attempt"))
		}
		boxes = append(boxes, box(t, "", sRed.Render(wrapCode(b.Answer, inner)), w, 0))
	}
	if b.HintsShown > 0 {
		var hs []string
		for i := 0; i < b.HintsShown && i < len(b.Hints); i++ {
			n := sYellow.Render(fmt.Sprintf("%d", i+1)) + "  "
			hs = append(hs, n+indentRest(ansi.Wrap(b.Hints[i], inner-3, " "), 3))
		}
		boxes = append(boxes, box(fmt.Sprintf("Hints %d/%d", b.HintsShown, len(b.Hints)), "", strings.Join(hs, "\n"), w, 0))
	}
	if b.Resolved() {
		content := sGreen.Render(wrapCode(b.Expected, inner))
		if b.Explanation != "" {
			content += "\n\n" + bold.Render("Why") + "\n" + ansi.Wrap(b.Explanation, inner, " ")
		}
		boxes = append(boxes, box("Answer", "", content, w, 0))
	}
	return lipgloss.JoinVertical(lipgloss.Left, boxes...)
}

// codeLines は今のファイルの穴の周辺を行番号付きで返す。長い行は折り返し、回答部分は追加行の色にする。
func (m *Model) codeLines(b *quiz.Blank, inner int) string {
	st := m.files[b.File]
	if st == nil {
		return dim.Render("Loading…")
	}
	lines := strings.Split(strings.TrimRight(string(st.content), "\n"), "\n")
	r, ok := m.regions[b.ID]
	if !ok {
		r = [2]int{b.Line - 1, b.Line}
	}
	from, to := max(0, r[0]-3), min(len(lines), r[1]+3)
	const gutter = 7
	var out []string
	for i := from; i < to; i++ {
		inBlank := i >= r[0] && i < r[1]
		text := strings.ReplaceAll(lines[i], "\t", "    ")
		wrapped := strings.Split(wrapCode(text, inner-gutter), "\n")
		for k, part := range wrapped {
			num := "     "
			if k == 0 {
				num = fmt.Sprintf("%5d", i+1)
			}
			sign := " "
			if inBlank {
				sign = "+"
			}
			row := dim.Render(num) + " " + sign + part
			if inBlank {
				row = lipgloss.NewStyle().Background(addBg).Width(inner).Render(num + " " + sign + part)
			}
			out = append(out, row)
		}
	}
	return strings.Join(out, "\n")
}

// box は枠の上辺にタイトルを埋め込んだ箱を描く。h が 0 なら中身に合わせる。
func box(title, right, content string, w, h int) string {
	inner := w - 4
	body := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder(), false, true, true, true).
		BorderForeground(border).
		Padding(0, 1).
		Width(w - 2)
	if h > 0 {
		body = body.Height(max(1, h-2))
	}
	t := " " + bold.Render(ansi.Truncate(title, max(0, inner-lipgloss.Width(right)-2), "…")) + " "
	r := ""
	if right != "" {
		r = " " + right + " "
	}
	fill := max(0, w-2-1-lipgloss.Width(t)-lipgloss.Width(r)-1)
	top := sBorder.Render("╭─") + t + sBorder.Render(strings.Repeat("─", fill)) + r + sBorder.Render("─╮")
	return top + "\n" + body.Render(content)
}

// コードは単語の区切りを気にせず幅で折る。単語単位で折るとインデントや記号の並びが崩れるため
func wrapCode(s string, w int) string {
	if w <= 1 {
		return s
	}
	return ansi.Hardwrap(strings.ReplaceAll(s, "\t", "    "), w, true)
}

func clip(s string, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= h {
		return s
	}
	return strings.Join(append(lines[:max(0, h-1)], dim.Render("  …")), "\n")
}

func indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

func indentRest(s string, n int) string {
	return strings.ReplaceAll(s, "\n", "\n"+strings.Repeat(" ", n))
}

// shortPath はホームを ~ に縮める。worktree のパスは長く、ヘッダーを圧迫するため
func shortPath(p string) string {
	if h, err := os.UserHomeDir(); err == nil && h != "" && strings.HasPrefix(p, h) {
		return "~" + strings.TrimPrefix(p, h)
	}
	return p
}
