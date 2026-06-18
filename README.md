# pr-log-reviewer

Uses a configurable AI provider (Copilot, Claude, or a local model) to review
the quality of a commit message or PR description against the actual change.

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

### Local model (`-provider local`)

Any OpenAI-compatible server (Ollama, llama.cpp, LM Studio, vLLM). No
credentials required. Defaults to Ollama at `http://localhost:11434/v1`;
override with `-base-url`.

```bash ollama pull llama3.2 go run ./cmd/provider-test -provider local -model
llama3.2 -prompt "Say hi" ```
