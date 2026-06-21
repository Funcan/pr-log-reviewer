# pr-log-reviewer

Uses a configurable AI provider (Copilot, Claude, or a local model) to review
the quality of a commit message or PR description against the actual change.

## Usage

Build the binaries with `make build` (output in `bin/`), or run directly with
`go run ./cmd/plr`.

`plr` reviews a commit, staged changes, or a pull request and prints a score
(1–5), per-category scores, and actionable findings.

```bash
# Review the latest commit
plr -commit HEAD

# Review a specific commit in another repo
plr -commit a1b2c3d -repo /path/to/repo

# Review staged changes before committing (pair with a candidate message)
git add .
plr -staged -message "Fix login timeout"

# Review a pull request (uses gh; must be authenticated)
plr -pr 42

# Machine-readable output
plr -commit HEAD -json

# Fail the command (exit 1) if the score is below a threshold — for CI/hooks
plr -commit HEAD -fail-under 3
```

### Flags

| Flag | Description |
| --- | --- |
| `-commit <ref>` | Review a commit by ref (default: `HEAD`). |
| `-staged` | Review the staged (index) changes. |
| `-pr <ref>` | Review a pull request by number, URL, or branch. |
| `-repo <dir>` | Git repository directory (default: current dir). |
| `-message <text>` | Message to pair with `-staged` changes. |
| `-provider <kind>` | AI provider: `copilot` (default), `github-models`, `anthropic`, `gemini`, `local`, `openai`. |
| `-model <name>` | Model name (default: `gpt-4o`). |
| `-base-url <url>` | Base URL override (local/openai/gemini). |
| `-api-key <key>` | API key/token (falls back to a provider-specific env var). |
| `-temperature <f>` | Sampling temperature (default: `0`). |
| `-max-tokens <n>` | Max tokens for the model response (default: `1500`). |
| `-max-diff-bytes <n>` | Truncate the diff to this many bytes (`0` = no limit). |
| `-max-retries <n>` | Retries when the model returns an unparseable response (default: `1`). |
| `-conventional` | Also require Conventional Commits formatting. |
| `-json` | Emit the review as JSON. |
| `-fail-under <n>` | Exit non-zero if the score is below `n` (`0` = never). |

The `change-extract` and `provider-test` binaries are development aids for
inspecting the extracted change and exercising a provider, respectively.

## Evaluation harness

The reviewer is only useful if its scores are reliable, so quality is measured
against a corpus of changes under `eval/corpus/`. Each case is a directory with
a `change.patch` (one diff) and a `case.yaml` listing several candidate
messages, each labelled `good`, `okay`, or `bad`:

- `good` — a strong message (expected to score `>= 4`); lists no defects.
- `okay` — mediocre but acceptable (expected to score in the `3-4` band); may
  optionally note minor defects. Excluded from pairwise and recall.
- `bad` — a poor message (expected to score `<= 2`); must tag the rubric
  categories (`defects:`) it violates.

A case must contain at least one `good` and one `bad` message.

To scaffold a case from a real commit, PR, or staged change, use `corpus-add`.
It writes `change.patch` and a `case.yaml` seeded with the real message (as an
unlabeled `quality: TODO` candidate) plus commented stubs; you then label it and
add good/bad alternatives:

```bash
# From a commit (writes eval/corpus/<id>/)
go run ./cmd/corpus-add -id divide-guard -commit HEAD

# From a pull request, or from staged changes
go run ./cmd/corpus-add -id my-pr -pr 123
go run ./cmd/corpus-add -id wip -staged -message "draft commit message"
```

The `eval` command runs the reviewer over the corpus and reports three metrics:

- **Pairwise discrimination** — within each case, does every good message
  outscore every bad one?
- **Absolute thresholds** — do good messages score `>= 4` and bad ones `<= 2`?
- **Defect recall** — of the tagged defects on bad messages, how many did the
  reviewer flag (via a finding in that category or a category score `<= 2`)?

It exits non-zero when any metric falls below its target (defaults: pairwise
`0.90`, threshold `0.80`, recall `0.70`), so it works as a CI gate.

```bash
# Replay recorded fixtures — deterministic, no network (CI default)
make eval
go run ./cmd/eval -mode replay

# Call the live provider and save fixtures for later replay
go run ./cmd/eval -mode record -provider copilot -model gpt-4o

# Call the live provider without recording
go run ./cmd/eval -mode live -provider copilot -model gpt-4o

# Inspect one case, with per-message progress and JSON output
go run ./cmd/eval -mode replay -case divide-guard -v -json
```

Useful flags: `-corpus`, `-fixtures`, `-good-min`/`-okay-min`/`-okay-max`/`-bad-max`
(threshold tuning), `-target-pairwise`/`-target-threshold`/`-target-recall`
(gate tuning).

## Credentials setup

Pick whichever provider you want to use and set its credentials. The provider
is selected with `-provider` (see `go run ./cmd/provider-test -h`).

### Copilot (`-provider copilot`)

Uses your existing GitHub Copilot subscription via the Copilot CLI's stored
credentials. Authenticate the Copilot CLI once and no further setup is needed -
the token is read from `~/.config/github-copilot/apps.json` automatically.

To override, set `COPILOT_OAUTH_TOKEN` to a GitHub OAuth token with Copilot
access.

Model names are bare, e.g. `gpt-4o`.

### GitHub Models (`-provider github-models`)

Requires a GitHub Personal Access Token with the **`models:read`** permission:

1. Create a fine-grained PAT at
   https://github.com/settings/personal-access-tokens/new
2. Under **Account permissions -> Models**, set **Read-only**.
3. Export it: `export GITHUB_MODELS_TOKEN=...` (falls back to `GITHUB_TOKEN`).

Model names use the `publisher/model` form, e.g. `openai/gpt-4o-mini`. Browse
available models at https://github.com/marketplace/models.

> Note: GitHub Models may be disabled for enterprise-managed accounts. If every
> model returns `no_access`, use `-provider copilot` instead.

### Anthropic / Claude (`-provider anthropic`)

```bash export ANTHROPIC_API_KEY=...  ```

Model names are e.g. `claude-3-5-sonnet-latest`.

### Google Gemini (`-provider gemini`)

Uses Gemini's OpenAI-compatible endpoint. Get an API key from Google AI Studio
(https://aistudio.google.com/apikey):

```bash
export GEMINI_API_KEY=...   # falls back to GOOGLE_API_KEY
```

Model names are e.g. `gemini-2.0-flash`.

### Local model (`-provider local`)

Any OpenAI-compatible server (Ollama, llama.cpp, LM Studio, vLLM). No
credentials required. Defaults to Ollama at `http://localhost:11434/v1`;
override with `-base-url`.

```bash ollama pull llama3.2 go run ./cmd/provider-test -provider local -model
llama3.2 -prompt "Say hi" ```
