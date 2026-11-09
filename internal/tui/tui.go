package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tknkaa/gh-prq/internal/lang"
	"github.com/tknkaa/gh-prq/internal/quiz"
	"github.com/tknkaa/gh-prq/internal/session"
)

type StepMsg struct {
	Step   int
	Detail string
}

type ReadyMsg struct {
	Session *session.Session
	Err     error
}

type tickMsg time.Time

type fileState struct {
	mod     time.Time
	size    int64
	content []byte
}

type Model struct {
	stepNames []string
	step      int
	details   []string
	frame     int

	s      *session.Session
	err    error
	cursor int

	files   map[string]*fileState
	orig    map[string][]byte
	regions map[int][2]int
	flash   string
	width   int
	height  int
}

func New(stepNames []string) *Model {
	return &Model{
		stepNames: stepNames,
		details:   make([]string, len(stepNames)),
		files:     map[string]*fileState{},
		orig:      map[string][]byte{},
		regions:   map[int][2]int{},
	}
}

// 保存の検知はポーリングで行う。fsnotify を入れるほどの差はなく、依存を増やしたくないため
const pollInterval = 100 * time.Millisecond

func tick() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) Init() tea.Cmd { return tick() }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case StepMsg:
		m.step = msg.Step
		if msg.Step < len(m.details) {
			m.details[msg.Step] = msg.Detail
		}
	case ReadyMsg:
		m.s, m.err = msg.Session, msg.Err
		if m.s != nil {
			for _, f := range m.s.Files() {
				if data, err := m.s.ReadFile("orig", f); err == nil {
					m.orig[f] = data
				}
			}
			m.poll()
		}
	case tickMsg:
		m.frame++
		if m.s != nil {
			m.poll()
		}
		return m, tick()
	case tea.KeyMsg:
		return m, m.key(msg.String())
	}
	return m, nil
}

func (m *Model) key(k string) tea.Cmd {
	switch k {
	case "q", "ctrl+c":
		return tea.Quit
	}
	if m.s == nil || len(m.s.Blanks) == 0 {
		return nil
	}
	b := &m.s.Blanks[m.cursor]
	m.flash = ""
	switch k {
	case "j", "down", "tab":
		m.cursor = (m.cursor + 1) % len(m.s.Blanks)
	case "k", "up", "shift+tab":
		m.cursor = (m.cursor - 1 + len(m.s.Blanks)) % len(m.s.Blanks)
	case "h":
		if b.HintsShown < len(b.Hints) {
			b.HintsShown++
		} else {
			m.flash = "No more hints."
		}
	case "a":
		// 自力で解いた後に答えを見ても「答えを見た」扱いにしない
		if !b.Correct {
			b.Revealed = true
		}
	case "m":
		if b.Answered && !b.Correct {
			b.Marked = true
		} else {
			m.flash = "Write an answer first."
		}
	case "y":
		m.flash = copyToClipboard(fmt.Sprintf("%s:%d", filepath.Join(m.s.Worktree, b.File), m.currentLine(b)))
	}
	_ = m.s.Save()
	return nil
}

func (m *Model) poll() {
	changed := false
	for _, f := range m.s.Files() {
		fi, err := os.Stat(filepath.Join(m.s.Worktree, f))
		if err != nil {
			continue
		}
		st := m.files[f]
		if st != nil && st.mod.Equal(fi.ModTime()) && st.size == fi.Size() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.s.Worktree, f))
		if err != nil {
			continue
		}
		m.files[f] = &fileState{mod: fi.ModTime(), size: fi.Size(), content: data}
		m.regrade(f, data)
		changed = true
	}
	if changed {
		_ = m.s.Save()
	}
}

func (m *Model) regrade(file string, data []byte) {
	l := lang.ForFile(file)
	for i := range m.s.Blanks {
		b := &m.s.Blanks[i]
		if b.File != file {
			continue
		}
		ex := quiz.Extract(data, b)
		b.Missing = !ex.Found
		if !ex.Found {
			continue
		}
		m.regions[b.ID] = [2]int{ex.FromRow, ex.ToRow}
		if !ex.Answered {
			b.Answered, b.Answer, b.Correct = false, "", false
			continue
		}
		if b.Answered && ex.Answer == b.Answer {
			continue
		}
		b.Answered, b.Answer = true, ex.Answer
		b.Attempts++
		b.Correct = quiz.Grade(l, m.orig[file], b, ex.Answer)
	}
}

func (m *Model) currentLine(b *quiz.Blank) int {
	if r, ok := m.regions[b.ID]; ok {
		return r[0] + 1
	}
	return b.Line
}

// GitHub の Primer に寄せた配色
var (
	green   = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	red     = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}
	yellow  = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"}
	blue    = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
	purple  = lipgloss.AdaptiveColor{Light: "#8250df", Dark: "#a371f7"}
	muted   = lipgloss.AdaptiveColor{Light: "#656d76", Dark: "#8d96a0"}
	addBg   = lipgloss.AdaptiveColor{Light: "#dafbe1", Dark: "#12261e"}
	selectB = lipgloss.AdaptiveColor{Light: "#ddf4ff", Dark: "#121d2f"}

	bold      = lipgloss.NewStyle().Bold(true)
	dim       = lipgloss.NewStyle().Foreground(muted)
	sGreen    = lipgloss.NewStyle().Foreground(green)
	sRed      = lipgloss.NewStyle().Foreground(red)
	sYellow   = lipgloss.NewStyle().Foreground(yellow)
	sBlue     = lipgloss.NewStyle().Foreground(blue)
	sPurple   = lipgloss.NewStyle().Foreground(purple)
	sAdd      = lipgloss.NewStyle().Background(addBg)
	sSelected = lipgloss.NewStyle().Background(selectB)
	sBadge    = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(green).Padding(0, 1).Bold(true)
)

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m *Model) View() string {
	if m.s == nil {
		return m.loadingView()
	}
	return m.mainView()
}

func (m *Model) loadingView() string {
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
			mark = " "
		}
		line := " " + mark + " " + name
		if d := m.details[i]; d != "" && i <= m.step {
			line += "  " + dim.Render(d)
		}
		b.WriteString(line + "\n")
	}
	if m.err != nil {
		b.WriteString("\n " + sRed.Render(m.err.Error()) + "\n\n " + dim.Render("Press q to quit.") + "\n")
	}
	return b.String()
}

func (m *Model) width0() int {
	if m.width <= 0 {
		return 100
	}
	return m.width
}

func (m *Model) mainView() string {
	s := m.s
	w := m.width0()
	var out []string

	title := bold.Render(s.Target.Title)
	if s.Target.PR > 0 {
		title += " " + dim.Render(fmt.Sprintf("#%d", s.Target.PR))
	}
	out = append(out, "", " "+title)
	resolved := 0
	for i := range s.Blanks {
		if s.Blanks[i].Resolved() {
			resolved++
		}
	}
	out = append(out, " "+dim.Render(fmt.Sprintf("%s → %s · %s · %d / %d resolved",
		s.Target.HeadRef, s.Target.BaseRef, s.Level, resolved, len(s.Blanks))))
	out = append(out, " "+dim.Render("Worktree ")+s.Worktree)
	for _, n := range s.Notes {
		out = append(out, " "+sYellow.Render("! "+n))
	}
	out = append(out, " "+strings.Repeat("─", max(0, w-2)))

	if s.AllResolved() {
		out = append(out, " "+sBadge.Render("All blanks resolved")+"  "+dim.Render("Press q to finish."), "")
	}

	// ファイルごとにまとめて、GitHub の Files changed のように見せる
	listRows := 0
	lastFile := ""
	for i := range s.Blanks {
		b := &s.Blanks[i]
		if b.File != lastFile {
			out = append(out, " "+bold.Render(b.File))
			lastFile = b.File
			listRows++
		}
		mark, label := outcomeMark(b)
		row := fmt.Sprintf("   %s #%-2d %s:%-5d %s", mark, b.ID, filepath.Base(b.File), m.currentLine(b), label)
		if i == m.cursor {
			row = sSelected.Render(padRight(row, w-1))
		}
		out = append(out, row)
		listRows++
	}
	out = append(out, " "+strings.Repeat("─", max(0, w-2)))
	out = append(out, m.detailView(&s.Blanks[m.cursor], w)...)

	help := dim.Render(" j/k move · h hint · a show answer · m mark as correct · y copy path · q quit")
	if m.flash != "" {
		help = " " + sYellow.Render(m.flash) + "  " + help
	}
	out = append(out, "", help)
	return strings.Join(out, "\n")
}

func outcomeMark(b *quiz.Blank) (string, string) {
	switch b.Outcome() {
	case "solved":
		return sGreen.Render("✓"), sGreen.Render("Solved")
	case "solved_with_hints":
		return sGreen.Render("✓"), sGreen.Render("Solved") + dim.Render(" with hints")
	case "marked":
		return sPurple.Render("✓"), sPurple.Render("Marked as correct")
	case "revealed":
		return sBlue.Render("◉"), sBlue.Render("Revealed")
	case "incorrect":
		return sRed.Render("✗"), sRed.Render("Incorrect")
	case "missing":
		return sYellow.Render("?"), sYellow.Render("Can't find the blank")
	}
	return dim.Render("·"), dim.Render("Not answered")
}

func (m *Model) detailView(b *quiz.Blank, w int) []string {
	var out []string
	_, label := outcomeMark(b)
	head := fmt.Sprintf(" %s  %s  %s", bold.Render(fmt.Sprintf("#%d", b.ID)),
		fmt.Sprintf("%s:%d", b.File, m.currentLine(b)), label)
	if b.Attempts > 0 {
		head += dim.Render(fmt.Sprintf(" · %d %s", b.Attempts, plural(b.Attempts, "attempt")))
	}
	out = append(out, head, "")
	out = append(out, m.contextLines(b, w)...)

	if b.Missing {
		out = append(out, "", " "+sYellow.Render("The lines around this blank were changed, so the answer can't be located."))
	}
	if b.Answered && !b.Correct && !b.Revealed && !b.Marked {
		out = append(out, "", " "+dim.Render("Yours")+"  "+firstLines(b.Answer, 6))
	}
	for i := 0; i < b.HintsShown && i < len(b.Hints); i++ {
		out = append(out, "", " "+sYellow.Render(fmt.Sprintf("Hint %d", i+1))+"  "+strings.ReplaceAll(b.Hints[i], "\n", "\n         "))
	}
	if b.Revealed || b.Correct || b.Marked {
		out = append(out, "", " "+dim.Render("Answer")+"  "+sGreen.Render(firstLines(b.Expected, 12)))
		if b.Explanation != "" {
			out = append(out, "", " "+dim.Render("Why")+"     "+strings.ReplaceAll(b.Explanation, "\n", "\n         "))
		}
	}
	return out
}

// contextLines は今のファイルの穴の周辺を、回答部分を追加行の色で表示する。
func (m *Model) contextLines(b *quiz.Blank, w int) []string {
	st := m.files[b.File]
	if st == nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(st.content), "\n"), "\n")
	r, ok := m.regions[b.ID]
	if !ok {
		r = [2]int{b.Line - 1, b.Line}
	}
	from, to := max(0, r[0]-3), min(len(lines), r[1]+3)
	var out []string
	for i := from; i < to; i++ {
		text := strings.ReplaceAll(lines[i], "\t", "    ")
		row := fmt.Sprintf(" %s  %s", dim.Render(fmt.Sprintf("%5d", i+1)), truncate(text, w-10))
		if i >= r[0] && i < r[1] {
			row = sAdd.Render(padRight(fmt.Sprintf(" %5d +%s", i+1, truncate(text, w-10)), w-1))
		}
		out = append(out, row)
	}
	return out
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = append(lines[:n], "…")
	}
	return strings.Join(lines, "\n        ")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if n <= 1 || len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func padRight(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

func copyToClipboard(s string) string {
	for _, c := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"pbcopy"}} {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stdin = strings.NewReader(s)
		if cmd.Run() == nil {
			return "Copied " + s
		}
	}
	return "No clipboard tool found: " + s
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
