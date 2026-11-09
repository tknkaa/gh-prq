package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
	case "w":
		// 別ペインで `cd <貼り付け>` するのが一番手数が少ないので、パスだけをコピーする
		m.flash = copyToClipboard(m.s.Worktree)
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
