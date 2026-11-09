---
name: prq
description: Quiz the user on a pull request with gh-prq. Pick the blanks that capture the PR's intent, open the quiz in a new terminal tab, and review the results afterwards. Use when the user wants to understand, review, or recall a PR by solving it ("quiz me on this PR", "I want to solve #123", "PR を解きたい").
---

# prq: fill-in-the-blank quizzes for pull requests

`gh prq` turns a PR diff into blanks that the user fills in with their own editor. Your job is to choose
blanks worth solving, launch the quiz, and explain the results. You never run the quiz logic yourself:
you only write a plan file and read a status file.

## 1. Understand the PR

```sh
gh pr view [<number>] --json number,title,body,headRefOid,baseRefName,url
gh pr diff [<number>]
```

Omit the number to use the PR of the current branch. Read the title, body and diff until you can say in
one sentence what the PR changes and why.

## 2. Choose blanks and write the plan

Pick 3 to 6 blanks that test whether the user understands the intent of the change, not trivia:
the condition that guards a new case, the argument that changes behaviour, the line that wires
things together. Avoid imports, renames, logging and formatting-only lines.

Line numbers must refer to the file at the PR head. Read the files there before writing the plan,
because the diff hunk offsets are easy to get wrong:

```sh
git fetch origin pull/<number>/head
git show <headRefOid>:<path> | cat -n
```

Write the plan inside the git directory so it never shows up in the working tree:

```sh
plan="$(git rev-parse --path-format=absolute --git-common-dir)/prq/plan-<number>.json"
mkdir -p "$(dirname "$plan")"
```

```json
{
  "blanks": [
    {
      "file": "src/cart.ts",
      "line": 14,
      "text": "(a, i) => a + i.price",
      "hint": "How is each item's price added to the total?",
      "explanation": "The total now sums price instead of counting items, which is the bug this PR fixes."
    },
    {
      "file": "src/cart.ts",
      "line": 20,
      "endLine": 22,
      "hint": "What should happen when the total exceeds the limit?"
    }
  ]
}
```

- `text`: an exact substring of that single line. That substring becomes the blank.
- Without `text`, the whole lines `line`..`endLine` (inclusive) become the blank.
- Keep at least 2 untouched lines between blanks in the same file. Closer ones are skipped.
- `hint` is shown first when the user asks for a hint. Write it as a question about intent and never
  give away the answer.
- `explanation` is shown after the blank is solved or revealed. Explain why the code is this way.

## 3. Launch the quiz

prq writes `<plan without .json>.status.json` as soon as the quiz is ready. It contains the worktree path.

Inside herdr (`HERDR_ENV=1`), open a new tab for the quiz and put an editor next to it:

1. Create a tab in the repository root: `herdr tab create --cwd <repo root> --label prq --focus`
2. Run `gh prq <number> --plan "$plan"` in the tab's pane with `herdr pane run`.
3. Wait until the status file exists, then read `worktree` from it.
4. Split the pane to the right with `--cwd <worktree>` and run `nvim` there.

Use the herdr skill if you need the exact output format of these commands.

Outside herdr, do not try to open terminals. Tell the user to run `gh prq <number> --plan "$plan"`
in another terminal and to open their editor in the worktree path that prq shows.

## 4. Review

When the user says they are done, read the status file. For each blank it has `outcome`
(`solved`, `solved_with_hints`, `marked`, `revealed`, `incorrect`, `unanswered`, `missing`),
`answer`, `expected`, `attempts`, the hints used, and your `explanation`.

Do not walk through every blank. Focus on the ones that were revealed, incorrect, or needed hints:
explain what the user's answer would have done and why the PR needs the expected code. Briefly
acknowledge the ones solved without help.
