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
gh prq clean                    # remove leftover prq worktrees and AI plans in this repository
gh prq --version
```

- `--level`: `easy` (default) / `normal` / `hard`
- `--count`: number of blanks, 5 by default
- Every run is a new quiz with blanks chosen at random. Small PRs have few candidates, so the same blanks may come up again.
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
- Nothing is stored on disk outside the worktree. Blank positions, expected answers, the original files used for grading and progress live only in memory while prq runs. A quiz is meant to be solved in one sitting, so there is no resuming, and records would only pile up.
  - The one exception is AI use: with `--plan`, results are written to `<plan>.status.json` for the review (see AI integration).
- On quit (`q` / Ctrl+C), the worktree is removed and a summary is printed. Answers are discarded with it.
- `gh prq clean` removes every worktree under `.worktree/prq-*` in the repository, and the AI plan directory `<git common dir>/prq/`. It is meant for what is left behind when prq or the AI agent is interrupted. Running it while a quiz is open in another pane removes that quiz's worktree too.
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

### Status (prq → AI)

- With `--plan foo.json`, results are also written to `foo.status.json`, so the AI can read them. This is the only file prq writes outside the worktree.
- The file is written as soon as the quiz is ready, so the AI can wait for it to learn the worktree path.
- Contents: worktree, target, level, whether it is done, warnings, and for each blank the outcome, answer, expected answer, attempts, hints used and explanation.

### Skill

`skills/prq/SKILL.md`, distributed as a Claude Code plugin. The repository root is both the plugin and its marketplace (`.claude-plugin/plugin.json` and `.claude-plugin/marketplace.json` with `"source": "./"`), so it installs with `/plugin install prq --marketplace tknkaa/gh-prq`. The plugin has no version, so every push to the default branch is an update.

1. Understand the intent of the PR with `gh pr view` and `gh pr diff`.
2. Choose 3–6 blanks at the heart of the change, confirm line numbers against the files at the head, and write the plan to `<git common dir>/prq/plan-<N>.json` (so it never appears in the working tree).
3. Inside herdr, open a new tab running `gh prq <N> --plan <plan>`, and once the status file appears, open nvim in the worktree in a pane next to it. Outside herdr, only tell the user the command to run.
4. When the user is done, read the status file and focus the review on blanks that were `revealed`, `incorrect`, or needed hints.
5. Delete the plan and status files. If the agent is interrupted before this, `gh prq clean` removes them.

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
