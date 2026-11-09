package gitx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func Git(dir string, args ...string) (string, error) { return run(dir, "git", args...) }

// MainRoot は main のワークツリーのルートを返す。
// 別の worktree の中で実行されても、出題用の worktree を同じ場所 (.worktree/) に作るため。
func MainRoot(dir string) (string, error) {
	common, err := Git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	return filepath.Dir(common), nil
}

type PR struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	HeadRefName string `json:"headRefName"`
	HeadRefOid  string `json:"headRefOid"`
	BaseRefName string `json:"baseRefName"`
}

// ViewPR は n == 0 のとき、今のブランチの PR を gh に解決させる。
func ViewPR(dir string, n int) (*PR, error) {
	args := []string{"pr", "view"}
	if n > 0 {
		args = append(args, strconv.Itoa(n))
	}
	args = append(args, "--json", "number,title,url,headRefName,headRefOid,baseRefName")
	out, err := run(dir, "gh", args...)
	if err != nil {
		return nil, err
	}
	var pr PR
	if err := json.Unmarshal([]byte(out), &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// FetchPR は PR の head のコミット SHA を返す。
// フォークからの PR でも取れるよう、ブランチ名ではなく pull/N/head を取得する。
func FetchPR(root string, pr *PR) (string, error) {
	if _, err := Git(root, "fetch", "--quiet", "origin", pr.BaseRefName); err != nil {
		return "", err
	}
	if _, err := Git(root, "fetch", "--quiet", "origin", fmt.Sprintf("pull/%d/head", pr.Number)); err != nil {
		return "", err
	}
	if _, err := Git(root, "cat-file", "-e", pr.HeadRefOid+"^{commit}"); err == nil {
		return pr.HeadRefOid, nil
	}
	return Git(root, "rev-parse", "FETCH_HEAD")
}

func DefaultBranch(dir string) string {
	if ref, err := Git(dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return ref
	}
	for _, b := range []string{"main", "master"} {
		if _, err := Git(dir, "rev-parse", "--verify", "--quiet", b); err == nil {
			return b
		}
	}
	return "main"
}

// AddedRows はファイルごとに、head 側で追加された行 (0 始まり) を返す。
func AddedRows(dir, base, head string) (map[string]map[int]bool, error) {
	// ユーザーの diff 設定 (noprefix や外部 diff) で出力形式が変わらないよう固定する
	out, err := Git(dir, "diff", "--unified=0", "--no-color", "--no-ext-diff",
		"--src-prefix=a/", "--dst-prefix=b/", base, head)
	if err != nil {
		return nil, err
	}
	res := map[string]map[int]bool{}
	var file string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "):
			p := strings.TrimPrefix(line, "+++ ")
			file = ""
			if p != "/dev/null" {
				file = strings.TrimPrefix(p, "b/")
			}
		case strings.HasPrefix(line, "@@ ") && file != "":
			fields := strings.Fields(line)
			if len(fields) < 3 {
				continue
			}
			start, count := parseRange(strings.TrimPrefix(fields[2], "+"))
			if res[file] == nil {
				res[file] = map[int]bool{}
			}
			for r := start; r < start+count; r++ {
				res[file][r-1] = true
			}
		}
	}
	return res, nil
}

func parseRange(s string) (int, int) {
	a, b, ok := strings.Cut(s, ",")
	start, _ := strconv.Atoi(a)
	count := 1
	if ok {
		count, _ = strconv.Atoi(b)
	}
	return start, count
}

func AddWorktree(root, path, sha string) error {
	if err := ensureExcluded(root); err != nil {
		return err
	}
	// ブランチを作らずに detach で作る。片付けるときに消し忘れたブランチが残らないように
	_, err := Git(root, "worktree", "add", "--quiet", "--detach", path, sha)
	return err
}

func RemoveWorktree(root, path string) error {
	// 出題用の worktree は穴を空けた時点で必ず dirty なので --force が要る
	if _, err := Git(root, "worktree", "remove", "--force", path); err != nil {
		return err
	}
	_, err := Git(root, "worktree", "prune")
	return err
}

func ensureExcluded(root string) error {
	common, err := Git(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	p := filepath.Join(common, "info", "exclude")
	data, _ := os.ReadFile(p)
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) == "/.worktree/" {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := ""
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + "/.worktree/\n")
	return err
}
