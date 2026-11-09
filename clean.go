package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tknkaa/gh-prq/internal/gitx"
)

// runClean は今のリポジトリにある prq の worktree と、AI 用の plan を全部消す。
// 通常はどちらも終了時に消えるが、異常終了すると残るので、それを一掃するためのもの。
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
	removed := 0
	for _, p := range paths {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		if err := gitx.RemoveWorktree(root, p); err != nil {
			fmt.Fprintf(os.Stderr, "prq: failed to remove %s: %v\n", p, err)
			continue
		}
		fmt.Println("removed", p)
		removed++
	}
	// AI が書いた plan と status は講評の後に消す決まりだが、途中で落ちると残るので、ここでも消す
	planDir, err := gitx.PlanDir(root)
	if err == nil {
		if _, err := os.Stat(planDir); err == nil {
			if err := os.RemoveAll(planDir); err != nil {
				return err
			}
			fmt.Println("removed", planDir)
			removed++
		}
	}
	if removed == 0 {
		fmt.Println("Nothing to clean in this repository.")
	}
	return nil
}
