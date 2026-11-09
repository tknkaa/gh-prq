package main

import (
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
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "prq:", err)
		os.Exit(1)
	}
}

func parseArgs(args []string) (options, error) {
	fs := flag.NewFlagSet("prq", flag.ContinueOnError)
	level := fs.String("level", "easy", "difficulty: easy, normal, hard")
	count := fs.Int("count", 5, "number of blanks")
	plan := fs.String("plan", "", "use blanks chosen in a JSON plan file (written by an AI agent)")
	fresh := fs.Bool("new", false, "start a new quiz instead of resuming an unfinished one")
	showVersion := fs.Bool("version", false, "print version")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: gh prq [<number>] [--level easy|normal|hard] [--count N] [--new] [--plan FILE]")
		fs.PrintDefaults()
	}
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
	o.Level, o.Count, o.Plan, o.New = lv, *count, *plan, *fresh
	return o, nil
}

type prepResult struct {
	s   *session.Session
	err error
}

func run(args []string) error {
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
	if !s.Done {
		fmt.Println("Your answers are saved. Run `gh prq` again to continue.")
	}
}
