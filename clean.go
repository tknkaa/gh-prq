package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tknkaa/gh-prq/internal/gitx"
	"github.com/tknkaa/gh-prq/internal/session"
)

// runClean は今のリポジトリにある prq の worktree を全部消す。
// 異常終了などで残った worktree を一掃するためのもので、回答は退避するので後から再開できる。
func runClean() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := gitx.MainRoot(cwd)
	if err != nil {
		return err
	}
	paths, err := gitx.Worktrees(root)
	if err != nil {
		return err
	}
	prefix := filepath.Join(root, ".worktree", "prq-")
	byPath := map[string]*session.Session{}
	for _, s := range session.All() {
		byPath[s.Worktree] = s
	}
	removed := 0
	for _, p := range paths {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		if s := byPath[p]; s != nil {
			saveAnswers(s)
		}
		if err := gitx.RemoveWorktree(root, p); err != nil {
			fmt.Fprintf(os.Stderr, "prq: failed to remove %s: %v\n", p, err)
			continue
		}
		fmt.Println("removed", p)
		removed++
	}
	if removed == 0 {
		fmt.Println("No prq worktrees in this repository.")
	}
	return nil
}
