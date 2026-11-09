package main

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tknkaa/gh-prq/internal/gitx"
	"github.com/tknkaa/gh-prq/internal/lang"
	"github.com/tknkaa/gh-prq/internal/quiz"
	"github.com/tknkaa/gh-prq/internal/session"
)

type options struct {
	PR    int
	Level quiz.Level
	Count int
	Plan  string
	New   bool
}

const (
	stepResolve = iota
	stepFetch
	stepWorktree
	stepParse
	stepChoose
)

var stepNames = []string{
	"Resolving pull request",
	"Fetching commits",
	"Creating worktree",
	"Parsing changed files",
	"Choosing blanks",
}

func prepare(cwd string, o options, progress func(step int, detail string)) (*session.Session, error) {
	progress(stepResolve, "")
	root, err := gitx.MainRoot(cwd)
	if err != nil {
		return nil, err
	}
	t, err := resolveTarget(cwd, root, o, progress)
	if err != nil {
		return nil, err
	}

	// AI の出題で始めるときや --new のときは、やりかけのセッションより新しい出題を優先する。
	// 古いセッションは残るが、再開するのは一番新しいセッションなので、次回からはこちらが続きになる
	if s := session.FindResumable(root, t, o.Level); s != nil && o.Plan == "" && !o.New {
		progress(stepWorktree, "resuming "+s.ID)
		if err := restore(s); err != nil {
			return nil, err
		}
		progress(len(stepNames), "")
		return s, nil
	}

	added, err := gitx.AddedRows(root, t.BaseSHA, t.HeadSHA)
	if err != nil {
		return nil, err
	}
	if len(added) == 0 {
		return nil, fmt.Errorf("no changes between %s and %s", t.BaseRef, t.HeadRef)
	}

	var plan *quiz.Plan
	if o.Plan != "" {
		if plan, err = quiz.LoadPlan(o.Plan); err != nil {
			return nil, err
		}
	}

	s := &session.Session{
		ID: session.NewID(), Repo: root, Target: t, Level: o.Level, CreatedAt: time.Now(),
	}
	if plan != nil {
		s.Level = "ai"
		s.StatusPath = strings.TrimSuffix(o.Plan, ".json") + ".status.json"
	}
	s.Worktree = filepath.Join(root, ".worktree", "prq-"+s.ID)
	progress(stepWorktree, s.Worktree)
	if err := gitx.AddWorktree(root, s.Worktree, t.HeadSHA); err != nil {
		return nil, err
	}
	// 途中で失敗したときに、穴を空けかけた worktree を残さない
	ok := false
	defer func() {
		if !ok {
			_ = gitx.RemoveWorktree(root, s.Worktree)
		}
	}()

	var cands []quiz.Candidate
	parsed := 0
	origs := map[string][]byte{}
	var picked []quiz.Candidate
	if plan != nil {
		progress(stepParse, fmt.Sprintf("%d planned blanks", len(plan.Blanks)))
		for _, e := range plan.Blanks {
			l := lang.ForFile(e.File)
			if l == nil {
				s.Notes = append(s.Notes, fmt.Sprintf("skipped %s: unsupported language", e.File))
				continue
			}
			src, ok := origs[e.File]
			if !ok {
				if src, err = os.ReadFile(filepath.Join(s.Worktree, e.File)); err != nil {
					s.Notes = append(s.Notes, fmt.Sprintf("skipped %s: %v", e.File, err))
					continue
				}
				origs[e.File] = src
			}
			c, err := quiz.PlanCandidate(l, src, e)
			if err != nil {
				s.Notes = append(s.Notes, "skipped "+err.Error())
				continue
			}
			cands = append(cands, c)
		}
		kept, dropped := quiz.FilterOverlaps(cands)
		for _, c := range dropped {
			s.Notes = append(s.Notes, fmt.Sprintf("skipped %s:%d: too close to another blank", c.File, c.StartRow+1))
		}
		picked = quiz.SortCandidates(kept)
	}
	for file, rows := range added {
		if plan != nil {
			break
		}
		l := lang.ForFile(file)
		if l == nil {
			continue
		}
		src, err := os.ReadFile(filepath.Join(s.Worktree, file))
		if err != nil {
			continue
		}
		parsed++
		progress(stepParse, fmt.Sprintf("%d files", parsed))
		origs[file] = src
		cands = append(cands, quiz.FindCandidates(file, l, src, rows, o.Level)...)
	}

	progress(stepChoose, "")
	if plan == nil {
		rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0))
		picked = quiz.Select(cands, o.Count, rng)
	}
	if len(picked) == 0 && plan != nil {
		return nil, fmt.Errorf("no usable blanks in the plan: %s", strings.Join(s.Notes, "; "))
	}
	if len(picked) == 0 {
		return nil, fmt.Errorf("no %s blanks found in %d supported changed files", o.Level, parsed)
	}
	for i := 0; i < len(picked); {
		j := i
		for j < len(picked) && picked[j].File == picked[i].File {
			j++
		}
		file := picked[i].File
		blanked, blanks := quiz.Build(file, lang.ForFile(file), origs[file], picked[i:j], len(s.Blanks)+1)
		if err := os.WriteFile(filepath.Join(s.Worktree, file), blanked, 0o644); err != nil {
			return nil, err
		}
		if err := s.WriteFile("orig", file, origs[file]); err != nil {
			return nil, err
		}
		if err := s.WriteFile("work", file, blanked); err != nil {
			return nil, err
		}
		s.Blanks = append(s.Blanks, blanks...)
		i = j
	}
	if err := s.Save(); err != nil {
		return nil, err
	}
	ok = true
	progress(len(stepNames), "")
	return s, nil
}

func resolveTarget(cwd, root string, o options, progress func(int, string)) (session.Target, error) {
	var t session.Target
	pr, err := gitx.ViewPR(cwd, o.PR)
	if err != nil {
		if o.PR > 0 {
			return t, err
		}
		// PR を作る前のブランチでも復習できるよう、デフォルトブランチとの差分にフォールバックする
		head, err := gitx.Git(cwd, "rev-parse", "HEAD")
		if err != nil {
			return t, err
		}
		branch, _ := gitx.Git(cwd, "rev-parse", "--abbrev-ref", "HEAD")
		t = session.Target{Title: branch, HeadRef: branch, BaseRef: gitx.DefaultBranch(cwd), HeadSHA: head}
		progress(stepFetch, "no pull request; comparing with "+t.BaseRef)
	} else {
		progress(stepFetch, fmt.Sprintf("#%d", pr.Number))
		head, err := gitx.FetchPR(root, pr)
		if err != nil {
			return t, err
		}
		t = session.Target{
			PR: pr.Number, Title: pr.Title, URL: pr.URL,
			HeadRef: pr.HeadRefName, BaseRef: "origin/" + pr.BaseRefName, HeadSHA: head,
		}
	}
	base, err := gitx.Git(root, "merge-base", t.BaseRef, t.HeadSHA)
	if err != nil {
		return t, err
	}
	t.BaseSHA = base
	return t, nil
}

// restore は保存しておいた回答込みのファイルで worktree を作り直す。
func restore(s *session.Session) error {
	if _, err := os.Stat(s.Worktree); err != nil {
		if err := gitx.AddWorktree(s.Repo, s.Worktree, s.Target.HeadSHA); err != nil {
			return err
		}
	}
	for _, f := range s.Files() {
		data, err := s.ReadFile("work", f)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(s.Worktree, f), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// cleanup は回答を退避してから worktree を消す。次回はこの退避から再開する。
func cleanup(s *session.Session) error {
	for _, f := range s.Files() {
		if data, err := os.ReadFile(filepath.Join(s.Worktree, f)); err == nil {
			_ = s.WriteFile("work", f, data)
		}
	}
	s.Done = s.AllResolved()
	if err := s.Save(); err != nil {
		return err
	}
	return gitx.RemoveWorktree(s.Repo, s.Worktree)
}
