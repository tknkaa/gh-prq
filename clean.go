package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tknkaa/gh-prq/internal/gitx"
)

// runClean は今のリポジトリにある prq の worktree を全部消す。
// 通常は終了時に消えるが、異常終了すると残るので、それを一掃するためのもの。
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
	if removed == 0 {
		fmt.Println("No prq worktrees in this repository.")
	}
	return nil
}
