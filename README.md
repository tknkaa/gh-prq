# gh-prq

Turn a pull request diff into a fill-in-the-blank quiz, and solve it by writing code in your own editor.

Reading a diff is easy to skim. Rewriting the important parts yourself is not. `gh prq` checks out the PR into a temporary worktree, blanks out parts of the code it added, and grades your answers every time you save, with your editor and LSP working as usual.

```
 Add tax to cart total #123
 feat/cart-total → origin/main · easy · 1 / 3 resolved
 Worktree /home/you/src/shop/.worktree/prq-3f9a1c2e
 ──────────────────────────────────────────────────────────
 src/cart.ts
   ✓ #1  cart.ts:6     Solved
   ✗ #2  cart.ts:11    Incorrect
   · #3  cart.ts:15    Not answered
 ──────────────────────────────────────────────────────────
 #2  src/cart.ts:11  Incorrect · 1 attempt

     8    const sum = active.reduce((a, i) => a + i.price, 0);
     9
    10    if (sum > LIMIT) {
    11 +    console.warn("wrong");
    12      throw new Error("limit exceeded");

 Yours  ("wrong")

 Hint 1  It's an argument list.

 j/k move · h hint · a show answer · m mark as correct · y copy path · q quit
```

## Install

```sh
gh extension install tknkaa/gh-prq
```

Prebuilt binaries are published for Linux (amd64, arm64), macOS (arm64, amd64) and Windows (amd64).

## Usage

Run it inside a repository:

```sh
gh prq                          # the PR of the current branch
gh prq 123                      # a specific PR
gh prq --level hard --count 8   # harder blanks, and more of them
```

If the current branch has no PR yet, `gh prq` quizzes you on its diff against the default branch.

Then, in another terminal:

```sh
cd <the worktree path shown by gh prq>
nvim src/cart.ts   # or any editor
```

Search for `__prq_` or `prq#` to find the blanks, fill them in and save. The TUI grades each blank as soon as the file is saved.

| Key | Action |
|---|---|
| `j` / `k` | move between blanks |
| `h` | show the next hint |
| `a` | show the answer |
| `m` | mark your answer as correct (for valid alternatives) |
| `y` | copy `path:line` of the blank |
| `q` | quit |

Quitting saves your answers and removes the worktree. Run `gh prq` again on the same PR to continue where you left off.

### Levels

| Level | Blanks |
|---|---|
| `easy` | expressions: arguments, conditions, calls, arrow functions |
| `normal` | whole lines |
| `hard` | whole blocks, such as the body of an `if` or a short function |

### Grading

Answers are compared by their syntax trees, so whitespace, line breaks, comments and trailing commas do not matter. Hints get stronger each time you ask: the kind of code, its shape (`(_, _) => _ + _._`), the identifiers it uses, and finally its first few characters.

### Supported languages

TypeScript, TSX, JavaScript, Go, Python and Rust.

## Using it with an AI agent

`gh prq` works on its own, but an agent can pick blanks that capture the intent of a PR instead of random ones. The agent writes a plan file and passes it with `--plan`:

```sh
gh prq 123 --plan plan.json
```

```json
{
  "blanks": [
    {
      "file": "src/cart.ts",
      "line": 14,
      "text": "(a, i) => a + i.price",
      "hint": "How is each item's price added to the total?",
      "explanation": "The total now sums prices instead of counting items."
    },
    { "file": "src/cart.ts", "line": 20, "endLine": 22 }
  ]
}
```

The agent's hint is shown first, and its explanation appears once a blank is solved or revealed. Results are written next to the plan as `plan.status.json` so the agent can review them with you.

A [Claude Code](https://claude.com/claude-code) skill that does all of this is included in [`skills/prq`](skills/prq/SKILL.md). Copy or link it into `~/.claude/skills/prq` and ask Claude to quiz you on a PR.

## Building from source

A C compiler is required because tree-sitter is built with cgo.

```sh
git clone https://github.com/tknkaa/gh-prq
cd gh-prq
go build -o gh-prq .
gh extension install .
```

See [docs/spec.md](docs/spec.md) for the full specification.
