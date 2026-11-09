# gh-prq specification

(As of 2026-11-09)

A TUI tool that turns a pull request diff into a fill-in-the-blank quiz that you solve by writing code in your own editor. The main use case is reviewing your own PRs; it also helps you understand other people's PRs.

## Principles

- Answers are written in your own editor. The blanked files live in a real git worktree, so your LSP (completion, go-to-definition, type errors) works as usual.
- The TUI only displays and grades. It does not integrate with any editor; it only watches files, so any editor works.
- It works without AI. Both blank selection and hints are generated mechanically. AI is an optional layer that improves the questions and the review.
- Keep dependencies minimal. The only external libraries are Bubble Tea / Lip Gloss (TUI) and go-tree-sitter with the language grammars (parsing). git and gh are invoked as subprocesses.

## Technology

- Written in Go and distributed as a gh extension (`tknkaa/gh-prq`, command `gh prq`).
  - gh requires extension repositories to be named `gh-*`.
  - The binary does not depend on gh to start and also runs standalone (`./gh-prq`).
- tree-sitter is written in C, so cgo is required.
  - In development you only need gcc.
  - Release binaries are built natively per OS in CI (see Release).
- Supported languages: TypeScript / TSX / JavaScript / Go / Python / Rust / Terraform (HCL).
  - Grammars are compiled into the binary, so each new language makes it larger.

| Dependency | Version |
|---|---|
| go-tree-sitter | v0.25.0 |
| tree-sitter-typescript | v0.23.2 |
| tree-sitter-go / javascript / python | v0.25.0 |
| tree-sitter-rust | v0.24.2 |
| bubbletea | v1.3.10 |
| lipgloss | v1.1.0 |

## Commands

```sh
gh prq                          # PR of the current branch
gh prq 123                      # a specific PR
gh prq --level hard --count 8   # difficulty and number of blanks
gh prq 123 --plan plan.json     # blanks chosen by an AI agent
gh prq --version
```

- `--level`: `easy` (default) / `normal` / `hard`
- `--count`: number of blanks, 5 by default
- Flags may also follow the PR number.

## Target diff

- For a PR:
  - `gh pr view --json` provides the title, head commit and base branch.
  - Commits are fetched with `git fetch origin <base>` and `git fetch origin pull/<N>/head`. `pull/N/head` is used instead of the branch name so that PRs from forks work.
  - The diff is `merge-base(origin/<base>, head)..head`.
- If the current branch has no PR, prq falls back to the diff against the default branch (`origin/HEAD`, otherwise `main` or `master`), so you can review a branch before opening a PR.
- The diff comes from git, not the GitHub API. The API truncates diffs of large PRs and could disagree with the worktree contents.
- Arbitrary ref ranges (`main..feat/x`) are not supported.

## Blanks

### Selection

Candidates are found with tree-sitter among the added lines.

| Level | Unit | Conditions |
|---|---|---|
| easy | expression | calls, argument lists, binary expressions, arrow functions, member access, parenthesized conditions, and so on. Fits on one line, 3–80 characters, and is not the whole line |
| normal | one line | the line's content matches exactly one syntax node. Lines cut mid-construct such as `foo(` are excluded because their answer is ambiguous |
| hard | block | the body of `{ }` (the brace lines stay) or a Python indented block. 2–12 lines, all of them added |

- Bare identifiers and literals are never blanks; they would be guesswork.
- Terraform (HCL) uses its own rules, because what matters there is which value each argument gets:
  - easy: the whole right-hand side of an argument (`name = <blank>`), such as a reference (`azurerm_resource_group.main.name`), a function call, a conditional or a string template. Parts of a reference are never blanked on their own. `true`, `false` and `null` are excluded because they are a coin toss.
  - normal: a whole `name = value` line.
  - hard: the body of a block (`resource`, `dynamic`, `content`, …), keeping the header and braces. The hint names the block type, such as "the body of a content block".
  - The Terraform dialect of the grammar has no Go binding, so the plain HCL grammar is used. Terraform syntax is plain HCL, so nothing is lost.
- Candidates are shuffled and picked so that at least 2 untouched lines separate blanks in the same file. Answers are located using the 2 lines before and after each blank as anchors, so anchors must not overlap other blanks.

### Representation in the file

- Expression blanks become `__prq_1__`. The code stays syntactically valid, so completion and type inference around it keep working.
- Line and block blanks become a single indented `// prq#2: fill in` line (`#` for Python). The `:` after the number prevents `#1` from matching `#10` as a prefix.
- The expected answer is never left in the file; it is stored in the session outside the worktree.

## Extracting answers

- Line numbers shift as the user edits, so the answer region is found using the 2 lines before and after the blank as anchors.
  - For the leading anchor, the match closest to the original position wins.
  - For the trailing anchor, the first match after the leading anchor wins.
- For expression blanks, the answer is the region minus the original text before and after the blank on that line.
- For line and block blanks, the region is the answer. A leftover placeholder comment is dropped, so content written below it is still graded.
- If the anchors cannot be found (for example, the surrounding lines were edited), the blank is reported as "Can't find the blank".

## Grading

- The answer is spliced back into the original file, and the sequence of syntax tree leaves is compared with the original.
  - Whitespace, newlines and comments are ignored.
  - A trailing comma before a closing bracket is ignored.
- Alternative correct answers (`a + b` vs `b + a`) are not detected automatically; the user can "Mark as correct" instead.
- prq does not run tests or builds to grade.

### Timing

- The modification time and size of each file with blanks are polled every 0.1 s, and the file is graded as soon as it changes. Any editor works as long as it saves the file.
- Polling is used instead of a file watcher library (fsnotify): there is no noticeable difference, and it avoids a dependency.
- An unchanged answer is not regraded. The attempt count increases every time the answer changes.

## Hints

Generated without AI. Each press reveals one more level.

| Level | Content | Example (answer `(a, i) => a + i.price`) |
|---|---|---|
| 1 | node kind | `It's an arrow function.` |
| 2 | shape, with identifiers, literals and strings replaced by `_` | `(_, _) => _ + _._` |
| 3 | identifiers used | `Uses: a, i, price` |
| 4 | first few characters | `Starts with: (a, i) …` |

When an AI agent writes the plan, its hint is added as the first level (5 levels in total).

## Outcomes

Each blank ends up in one of these states.

| outcome | Meaning |
|---|---|
| `solved` | correct without hints |
| `solved_with_hints` | correct after using hints |
| `marked` | graded incorrect, but the user marked it as correct |
| `revealed` | the answer was shown before solving |
| `incorrect` | answered but wrong |
| `unanswered` | not answered yet |
| `missing` | the answer could not be located |

- The expected answer stays hidden until "Show answer".
- Showing the answer after solving does not turn a blank into `revealed`.

## Sessions

- Worktree: `<root of the main worktree>/.worktree/prq-<id>`
  - Always created under the main worktree, whichever worktree prq is run from.
  - Created detached, without a branch, so that cleanup never leaves branches behind.
  - `/.worktree/` is appended to `.git/info/exclude` if missing.
- State: `$XDG_DATA_HOME/prq/sessions/<id>/` (default `~/.local/share/prq/sessions/<id>/`)
  - `state.json`: everything, including blank positions, answers, anchors, hints and progress
  - `status.json`: a summary for AI review
  - `orig/<file>`: the file before blanking (used for grading)
  - `work/<file>`: the file with the user's answers (used for resuming)
- On quit (`q` / Ctrl+C):
  1. Files with blanks are saved to `work/`.
  2. The session is marked done if every blank is resolved.
  3. The worktree is removed.
  4. A summary is printed.
- On the next run:
  - If an unfinished session exists for the same repository, PR, head commit and level, the worktree is recreated, overwritten with the files in `work/`, and the quiz continues.
  - If the head has changed, the diff has changed too, so a new quiz is created.
  - Only files with blanks are saved, so edits to other files are lost.
- Quitting during preparation waits for preparation to finish and then cleans up, so no worktree is left behind. A failed preparation also removes its worktree.

## TUI

All text is in English. There is no GitHub-style diff view; the screen shows only what is needed. On terminals at least 90 columns wide, the blank list sits on the left and the details on the right; narrower terminals stack them. Long lines wrap instead of being cut off: code wraps at the column, prose at word boundaries.

- While preparing: progress per step (Resolving pull request → Fetching commits → Creating worktree → Parsing changed files → Choosing blanks). A failed step shows ✗ and the reason.
- Main screen:
  - Header: PR title and number, `head → base`, level, number resolved, absolute worktree path
  - Warnings when parts of an AI plan could not be used
  - Blanks grouped by file: status mark, number, `file:line`, status
  - Selected blank: 3 lines of context around it (answer highlighted), your answer (when incorrect), hints shown so far, and the answer with the AI explanation (once resolved)
- Keys:

| Key | Action |
|---|---|
| `j` / `k` | move between blanks |
| `h` | next hint |
| `a` | show answer |
| `m` | mark as correct (only when answered and incorrect) |
| `w` | copy the worktree path to the clipboard, to `cd` into it from another pane |
| `y` | copy `absolute path:line` to the clipboard (wl-copy / xclip / pbcopy) |
| `q` / Ctrl+C | quit |

- Colors follow GitHub's Primer palette and adapt to light and dark terminals.

## AI integration

The AI never runs the quiz or grading logic. It communicates with prq through files.

### Plan (AI → prq)

```json
{
  "blanks": [
    { "file": "src/cart.ts", "line": 14, "text": "(a, i) => a + i.price",
      "hint": "How is each item's price added?", "explanation": "..." },
    { "file": "src/cart.ts", "line": 20, "endLine": 22 }
  ]
}
```

- `line` / `endLine`: 1-based line numbers in the file at the PR head
- `text`: a substring of that line. When given, only that substring becomes the blank; otherwise lines `line`..`endLine` become the blank.
- `hint`: the first hint. It should be a question about intent that does not give the answer away.
- `explanation`: shown as "Why" once the blank is resolved.
- Each entry is checked against the actual file. Entries with an out-of-range line, a missing text, or a position too close to another blank are skipped, and the reason is shown in the TUI.
- If the range matches a syntax node exactly, its kind is used in the hints; otherwise it is called a "piece of code".
- With `--plan`, a new quiz is always created, even if an unfinished session exists.

### Status (prq → AI)

- With `--plan foo.json`, results are also written to `foo.status.json`, so the AI can read them without knowing the session ID.
- The file is written as soon as the quiz is ready, so the AI can wait for it to learn the worktree path.
- Contents: worktree, target, level, whether it is done, warnings, and for each blank the outcome, answer, expected answer, attempts, hints used and explanation.

### Skill

`skills/prq/SKILL.md`:

1. Understand the intent of the PR with `gh pr view` and `gh pr diff`.
2. Choose 3–6 blanks at the heart of the change, confirm line numbers against the files at the head, and write the plan to `<git common dir>/prq/plan-<N>.json` (so it never appears in the working tree).
3. Inside herdr, open a new tab running `gh prq <N> --plan <plan>`, and once the status file appears, open nvim in the worktree in a pane next to it. Outside herdr, only tell the user the command to run.
4. When the user is done, read the status file and focus the review on blanks that were `revealed`, `incorrect`, or needed hints.

## Release

- Pushing a `v*` tag makes GitHub Actions build binaries natively on each OS with cgo and publish a release.
- Files are named `gh-prq_<tag>_<os>-<arch>[.exe]`; gh picks the binary for the user's platform by this suffix.

| OS / arch | Runner | Notes |
|---|---|---|
| linux-amd64 | ubuntu-22.04 | an older Ubuntu for glibc compatibility |
| linux-arm64 | ubuntu-22.04-arm | |
| darwin-arm64 | macos-latest | |
| darwin-amd64 | macos-latest | built with `clang -arch x86_64`; not smoke-tested |
| windows-amd64 | windows-latest | |

- Each binary is smoke-tested with `--version` on its runner (except darwin-amd64).
- gofmt, vet and tests run on every push and pull request.

## Unverified and future work

- The real PR path (`gh pr view`, fetching `pull/N/head`) has not been tried yet.
- The macOS release builds have not succeeded yet (the Linux and Windows builds have).
- The herdr steps of the skill have not been tried. Other multiplexers (such as tmux) will be supported on request.
- Installing with `gh extension install .` has not been tried.
