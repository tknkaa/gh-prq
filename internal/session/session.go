package session

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/tknkaa/gh-prq/internal/quiz"
)

type Target struct {
	PR      int    `json:"pr,omitempty"`
	Title   string `json:"title"`
	URL     string `json:"url,omitempty"`
	HeadRef string `json:"headRef"`
	BaseRef string `json:"baseRef"`
	HeadSHA string `json:"headSha"`
	BaseSHA string `json:"baseSha"`
}

type Session struct {
	ID        string     `json:"id"`
	Repo      string     `json:"repo"`
	Worktree  string     `json:"worktree"`
	Target    Target     `json:"target"`
	Level     quiz.Level `json:"level"`
	CreatedAt time.Time  `json:"createdAt"`
	Done      bool       `json:"done"`
	// Notes は出題時の警告 (AI の指定を採用できなかった理由など)
	Notes []string `json:"notes,omitempty"`
	// StatusPath を指定すると status.json をそこにも書く。AI がセッション ID を知らなくても結果を読めるように
	StatusPath string       `json:"statusPath,omitempty"`
	Blanks     []quiz.Blank `json:"blanks"`
}

// 状態を repo の外に置くのは、worktree を消しても回答や履歴が残るようにするため
func baseDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "prq")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "share", "prq")
}

func NewID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Session) Dir() string { return filepath.Join(baseDir(), "sessions", s.ID) }

func (s *Session) Files() []string {
	var files []string
	seen := map[string]bool{}
	for _, b := range s.Blanks {
		if !seen[b.File] {
			seen[b.File] = true
			files = append(files, b.File)
		}
	}
	return files
}

func (s *Session) AllResolved() bool {
	for i := range s.Blanks {
		if !s.Blanks[i].Resolved() {
			return false
		}
	}
	return true
}

func (s *Session) Save() error {
	if err := os.MkdirAll(s.Dir(), 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(s.Dir(), "state.json"), s); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(s.Dir(), "status.json"), s.status()); err != nil {
		return err
	}
	if s.StatusPath != "" {
		return writeJSON(s.StatusPath, s.status())
	}
	return nil
}

type blankStatus struct {
	ID        int      `json:"id"`
	File      string   `json:"file"`
	Line      int      `json:"line"`
	Kind      string   `json:"kind"`
	Outcome   string   `json:"outcome"`
	Attempts  int      `json:"attempts"`
	HintsUsed int      `json:"hintsUsed"`
	Answer    string   `json:"answer,omitempty"`
	Expected  string   `json:"expected"`
	Hints     []string `json:"hints,omitempty"`
	// Explanation は AI が出題時に書いた解説。講評のときに AI 自身が参照する
	Explanation string `json:"explanation,omitempty"`
}

// status は AI が講評に使う要約。state.json はアンカーなど内部用の情報が多いので分けている
func (s *Session) status() any {
	var bs []blankStatus
	for i := range s.Blanks {
		b := &s.Blanks[i]
		bs = append(bs, blankStatus{
			ID: b.ID, File: b.File, Line: b.Line, Kind: b.Kind, Outcome: b.Outcome(),
			Attempts: b.Attempts, HintsUsed: b.HintsShown, Answer: b.Answer, Expected: b.Expected,
			Hints: b.Hints[:min(b.HintsShown, len(b.Hints))], Explanation: b.Explanation,
		})
	}
	return map[string]any{
		"session": s.ID, "worktree": s.Worktree, "target": s.Target,
		"level": s.Level, "done": s.Done, "notes": s.Notes, "blanks": bs,
	}
}

func writeJSON(path string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// コードには < > & が頻出するので、AI や人が読むときに \u003e のようなエスケープが混ざらないようにする
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	// 書き込み途中で落ちても壊れた JSON が残らないよう、rename で差し替える
	return os.Rename(tmp, path)
}

// WriteFile は kind ("orig" か "work") ごとにファイルの中身を保存する。
func (s *Session) WriteFile(kind, file string, data []byte) error {
	p := filepath.Join(s.Dir(), kind, file)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func (s *Session) ReadFile(kind, file string) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.Dir(), kind, file))
}

// FindResumable は同じ PR の同じコミットに対する、やりかけのセッションを探す。
// head が進んでいたら差分が変わっているので、古いセッションは再開しない。
func FindResumable(repo string, t Target) *Session {
	entries, err := os.ReadDir(filepath.Join(baseDir(), "sessions"))
	if err != nil {
		return nil
	}
	var found *Session
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(baseDir(), "sessions", e.Name(), "state.json"))
		if err != nil {
			continue
		}
		var s Session
		if json.Unmarshal(data, &s) != nil {
			continue
		}
		if s.Done || s.Repo != repo || s.Target.HeadSHA != t.HeadSHA || s.Target.PR != t.PR || s.Target.HeadRef != t.HeadRef {
			continue
		}
		if found == nil || s.CreatedAt.After(found.CreatedAt) {
			found = &s
		}
	}
	return found
}
