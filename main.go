package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tknkaa/gh-prq/internal/quiz"
	"github.com/tknkaa/gh-prq/internal/session"
	"github.com/tknkaa/gh-prq/internal/tui"
)

var version = "dev"

func main() {
	err := run(os.Args[1:])
	// ヘルプの表示は失敗ではないので、エラーとして扱わず正常終了する
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "prq:", err)
		os.Exit(1)
	}
}

const usage = `gh prq turns a pull request diff into a fill-in-the-blank quiz that you
solve in your own editor.

Usage:
  gh prq [<number>] [flags]   Start a quiz on a pull request
  gh prq clean                Remove leftover prq worktrees and AI plans
  gh prq help                 Show this help

Without <number>, the pull request of the current branch is used. If the
branch has no pull request, its diff against the default branch is used.

Flags:
  --level <level>   Difficulty: easy, normal or hard (default "easy")
                      easy    expressions: arguments, conditions, calls
                      normal  whole lines
                      hard    whole blocks
  --count <n>       Number of blanks (default 5)
  --plan <file>     Use blanks chosen by an AI agent in a JSON plan file
  --version         Print the version
  -h, --help        Show this help

Keys:
  j/k  move between blanks     a  show the answer
  h    show the next hint      m  mark your answer as correct
  w    copy the worktree path  y  copy path:line of the blank
  q    quit (removes the worktree)

Solve it by opening the worktree in another terminal (press w to copy its
path), filling in the blanks and saving. Each save is graded immediately.

Examples:
  gh prq
  gh prq 123 --level hard --count 8
  gh prq clean

https://github.com/tknkaa/gh-prq
`

func parseArgs(args []string) (options, error) {
	fs := flag.NewFlagSet("prq", flag.ContinueOnError)
	level := fs.String("level", "easy", "difficulty: easy, normal, hard")
	count := fs.Int("count", 5, "number of blanks")
	plan := fs.String("plan", "", "use blanks chosen in a JSON plan file (written by an AI agent)")
	showVersion := fs.Bool("version", false, "print version")
	// 標準のフラグ一覧ではサブコマンドやキー操作が伝わらないので、自前のヘルプを出す
	fs.Usage = func() { fmt.Fprint(os.Stdout, usage) }
	var o options
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	// `gh prq 123 --level hard` のように PR 番号の後ろにフラグを書けるよう、残りをもう一度パースする
	if rest := fs.Args(); len(rest) > 0 {
		n, err := strconv.Atoi(rest[0])
		if err != nil {
			return o, fmt.Errorf("invalid pull request number %q", rest[0])
		}
		o.PR = n
		if err := fs.Parse(rest[1:]); err != nil {
			return o, err
		}
	}
	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}
	lv, err := quiz.ParseLevel(*level)
	if err != nil {
		return o, err
	}
	o.Level, o.Count, o.Plan = lv, *count, *plan
	return o, nil
}

type prepResult struct {
	s   *session.Session
	err error
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "clean":
			return runClean()
		case "help":
			fmt.Print(usage)
			return nil
		}
	}
	o, err := parseArgs(args)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	m := tui.New(stepNames)
	p := tea.NewProgram(m, tea.WithAltScreen())
	done := make(chan prepResult, 1)
	go func() {
		s, err := prepare(cwd, o, func(step int, detail string) {
			p.Send(tui.StepMsg{Step: step, Detail: detail})
		})
		done <- prepResult{s, err}
		p.Send(tui.ReadyMsg{Session: s, Err: err})
	}()
	_, runErr := p.Run()
	// 準備中に q で抜けても worktree を残さないよう、準備の完了を待ってから片付ける
	res := <-done
	if res.err != nil {
		return res.err
	}
	if err := cleanup(res.s); err != nil {
		return err
	}
	printSummary(res.s)
	return runErr
}

func printSummary(s *session.Session) {
	counts := map[string]int{}
	for i := range s.Blanks {
		counts[s.Blanks[i].Outcome()]++
	}
	fmt.Printf("prq: %d solved (%d with hints), %d marked, %d revealed, %d unresolved\n",
		counts["solved"]+counts["solved_with_hints"], counts["solved_with_hints"],
		counts["marked"], counts["revealed"],
		len(s.Blanks)-counts["solved"]-counts["solved_with_hints"]-counts["marked"]-counts["revealed"])
}
