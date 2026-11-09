package session

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"

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

// Session は 1 回の出題の状態。毎回新しく出題して途中から再開はしないので、ディスクには保存しない。
type Session struct {
	ID       string
	Repo     string
	Worktree string
	Target   Target
	Level    quiz.Level
	// Notes は出題時の警告 (AI の指定を採用できなかった理由など)
	Notes []string
	// StatusPath を指定すると、AI が講評に使う結果をそこに書く
	StatusPath string
	Blanks     []quiz.Blank
	// Orig は穴を空ける前のファイル。採点で回答を埋め戻すのに使う
	Orig map[string][]byte
}

func NewID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

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

// Save は AI と使う場合だけ結果を書き出す。単独で使う場合は何も書かない。
func (s *Session) Save() error {
	if s.StatusPath == "" {
		return nil
	}
	return writeJSON(s.StatusPath, s.status())
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
		"level": s.Level, "done": s.AllResolved(), "notes": s.Notes, "blanks": bs,
	}
}

func writeJSON(path string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// コードには < > & が頻出するので、AI や人が読むときに > のようなエスケープが混ざらないようにする
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	// 書き込み途中で読まれても壊れた JSON が見えないよう、rename で差し替える
	return os.Rename(tmp, path)
}
