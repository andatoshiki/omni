# AGENTS.md — Omni

> Rules and context for AI coding agents working on this project.

---

## 1. Project Overview

**Omni** is a self-hosted Telegram AI assistant written in Go.
It connects Telegram chats to multiple LLM providers (Anthropic, AWS Bedrock,
Azure, Cloudflare, Cohere, DeepSeek, Google Gemini, Groq, HuggingFace, Mistral,
Ollama, OpenAI, Perplexity, Together AI, xAI, and custom OpenAI-compatible
endpoints), supports streaming responses, multimodal media, persistent
conversation history, per-chat model selection, and token-based usage tracking.

| Attribute        | Value                                     |
| ---------------- | ----------------------------------------- |
| Module path      | `github.com/andatoshiki/omni`             |
| Go version       | 1.26.4                                    |
| Binary name      | `omni`                                    |
| License          | GPL-3.0                                   |
| Config format    | YAML (`config.yaml`)                      |
| Database         | SQLite (default), MySQL, PostgreSQL       |
| Release tooling  | GoReleaser v2                             |
| CI               | GitHub Actions (test, lint, build, Docker) |

---

## 2. Architecture

### 2.1 Package Layout

```text
.
├── main.go                         # Entry point: flags, init, signal handling
├── internal/
│   ├── bot/                        # Telegram update routing, commands, streaming, media
│   ├── command/                    # Command handler implementations (per-command files)
│   ├── config/                     # YAML loading, strict validation, defaults
│   ├── conversation/               # Domain types (Message, Speaker, ReplyContext)
│   ├── logging/                    # Structured slog configuration
│   ├── providers/                  # Provider registry, model resolution, Adapter interface
│   │   └── platforms/              # Per-provider adapter implementations
│   │       ├── anthropic/
│   │       ├── azureopenai/
│   │       ├── bedrock/
│   │       ├── cloudflare/
│   │       ├── cohere/
│   │       ├── custom/
│   │       ├── deepseek/
│   │       ├── google/
│   │       ├── groq/
│   │       ├── mistral/
│   │       ├── ollama/
│   │       ├── openai/
│   │       ├── together/
│   │       └── xai/
│   ├── storage/                    # Store interface + SQLite/MySQL/Postgres backends
│   ├── telegramhtml/               # Markdown → Telegram-safe HTML conversion
│   ├── update/                     # Self-update mechanism (GitHub releases)
│   └── version/                    # Build-time version variables (ldflags)
├── .github/
│   ├── workflows/ci.yml            # Test + lint + build + Docker test-build
│   ├── workflows/release.yml       # GoReleaser + GHCR Docker push on tag
│   └── dependabot.yml              # Weekly gomod + github-actions updates
├── .goreleaser.yaml                # Cross-platform release (linux/darwin/windows × amd64/arm64)
├── Dockerfile                      # Multi-stage: golang:1.26 builder → alpine runtime
├── config.yaml.example             # Annotated reference configuration
└── data/                           # Runtime database directory (gitignored)
```

### 2.2 Key Interfaces

| Interface                          | Package              | Purpose                                              |
| ---------------------------------- | -------------------- | ---------------------------------------------------- |
| `storage.Store`                    | `internal/storage`   | All DB operations (sessions, usage, models, export)  |
| `providers.Adapter`               | `internal/providers` | Provider-specific streaming chat completion boundary |
| `platforms.ChatCompletionStream`   | `platforms`          | SSE stream `Recv()`/`Close()` for token-by-token I/O |

### 2.3 Dependency Flow

```text
main.go
  → config.Params.Init()          (YAML load + validate)
  → providers.NewRegistry()       (build adapter per enabled provider)
  → storage.Open()                (select backend by config.Backend)
  → bot.New()                     (wire Telegram client, commands, aggregator)
  → app.Run(ctx)                  (getMe → register commands → poll updates)
```

All packages live under `internal/` — nothing is exported to external consumers.

---

## 3. Build, Test, and Lint

### 3.1 Canonical Commands

```bash
# Build
go build -o omni .

# Run all tests (always use -race in development)
go test -race ./...

# Run tests for a specific package
go test ./internal/bot
go test ./internal/config
go test ./internal/providers/...
go test ./internal/storage
go test ./internal/telegramhtml

# Format (rewrites files in-place)
go fmt ./...

# Vet
go vet ./...

# Lint (must pass CI)
golangci-lint run --timeout=5m

# Tidy modules
go mod tidy
```

### 3.2 Build Tags and ldflags

Production builds inject version metadata via ldflags:

```bash
go build -ldflags "-s -w \
  -X github.com/andatoshiki/omni/internal/version.Version=${VERSION} \
  -X github.com/andatoshiki/omni/internal/version.Commit=${COMMIT} \
  -X github.com/andatoshiki/omni/internal/version.BuildTime=${BUILD_TIME}" \
  -o omni .
```

The Dockerfile uses `CGO_ENABLED=0` for a static binary; GoReleaser adds
`-tags netgo,osusergo` for fully static cross-compilation.

### 3.3 CI Pipeline

CI runs on every push/PR to `main`/`master`:

1. **Test** — `go test -race -v ./...`
2. **Lint** — `golangci-lint run --timeout=5m`
3. **Build** — `go build -v -o omni .`
4. **Docker** — test-only image build (no push)

Release workflow triggers on `v*` tags and runs GoReleaser + Docker push to GHCR.

---

## 4. Code Conventions

### 4.1 Go Style

Follow the [Effective Go](https://go.dev/doc/effective_go), [Google Go Style Guide](https://google.github.io/styleguide/go/),
and [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
conventions. Key points for this project:

- **Formatting**: All code must pass `go fmt`. No exceptions.
- **Naming**: Use `MixedCaps` / `mixedCaps`. No underscores in Go names.
  Acronyms like `ID`, `URL`, `API`, `HTML`, `SSE` are all-caps.
- **Package names**: Lowercase, single-word, no underscores. Package name
  should not repeat the parent directory (e.g., `platforms/openai`, not
  `platforms/openaiplatform`). Import aliases are used when the package name
  collides (e.g., `openaiplatform "…/platforms/openai"`).
- **Error handling**: Always check errors. Wrap with `fmt.Errorf("context: %w", err)`.
  Never use `panic` for expected failure paths.
- **Error messages**: Start with lowercase, no trailing punctuation.
  Example: `fmt.Errorf("resolve config path %q: %w", path, err)`.
- **Receiver names**: Short, consistent, 1-2 letters (e.g., `a` for `*App`,
  `r` for `*Registry`, `s` for `*sseStream`).
- **Unexported types**: Use unexported types when the symbol is only used
  within its own package (e.g., `configFile`, `sseStream`).

### 4.2 Logging

- Use `log/slog` (structured logging) exclusively. Never use `fmt.Print*` for
  operational logging.
- Log level is controlled via `LOG_LEVEL` env var (`debug`, `info`, `warn`, `error`).
- Log format is controlled via `LOG_FORMAT` env var (`text` default, `json`).
- **Privacy**: Never log prompt content, response bodies, or API keys. Log
  identifiers, message sizes, model names, timing, and token counts only.
- Use `logging.TextMetricAttrs()` for safe text size metrics.

### 4.3 Configuration

- The YAML parser uses `KnownFields(true)` — unknown fields cause a startup
  failure. This is intentional and must not be changed.
- Multiple YAML documents in a single file are rejected.
- Validation errors must be descriptive and reference the exact config path
  (e.g., `providers[2].models[0].temperature`).

### 4.4 Imports

Organize imports in three groups separated by blank lines:

```go
import (
    // 1. Standard library
    "context"
    "fmt"

    // 2. Third-party dependencies
    telegram "github.com/go-telegram/bot"

    // 3. Internal packages
    "github.com/andatoshiki/omni/internal/config"
    "github.com/andatoshiki/omni/internal/providers"
)
```

### 4.5 Testing

- Test files live alongside the code they test (`*_test.go`).
- Use table-driven tests with `t.Run()` subtests.
- Test function names: `TestFunctionName_scenario` or `TestTypeName_MethodName`.
- No test frameworks — use stdlib `testing` only.
- Use `-race` flag during development and CI.
- Tests must not make network calls or require external services.

---

## 5. Provider Adapter Pattern

### 5.1 How to Add a New Provider

1. **Create a package** under `internal/providers/platforms/<name>/`.
2. **Implement the `Adapter` interface**:
   ```go
   type Adapter interface {
       CreateChatCompletionStream(
           ctx context.Context,
           endpoint platforms.Endpoint,
           request *platforms.ChatCompletionStreamRequest,
       ) (platforms.ChatCompletionStream, error)
   }
   ```
3. If the provider is OpenAI-compatible, embed `openaiplatform.Adapter`
   and override only what differs:
   ```go
   type Adapter struct {
       OpenAI openaiplatform.Adapter
   }
   ```
4. **Add a provider type constant** in `internal/config/provider.go`.
5. **Register the adapter** in `adapterForType()` in
   `internal/providers/registry.go`.
6. **Add the default base URL** to `defaultBaseURLs` in the same file.
7. **Update `validate()`** in `internal/config/config.go` to accept the new
   type in the provider type switch.
8. **Add a commented example** in `config.yaml.example`.

### 5.2 Adapter Embedding Pattern

Most providers use the OpenAI-compatible streaming protocol. In this project,
thin adapter wrappers embed `openaiplatform.Adapter` and only override behavior
as needed. Providers with native APIs (Anthropic, Google, Bedrock, Cohere)
have standalone adapter implementations.

---

## 6. Storage Backend Pattern

### 6.1 How to Add a New Database Backend

1. Create `internal/storage/<backend>.go` implementing `storage.Store`.
2. Add the backend string to `storage.Open()` switch.
3. Add a config struct in `internal/config/database.go`.
4. Wire it into `databaseConfig` (unexported YAML struct) and `DatabaseConfig`
   (exported runtime struct).
5. Add validation in `config.validate()` if the backend has required fields.

All three current backends (SQLite, MySQL, PostgreSQL) follow identical table
schemas and query patterns — maintain this consistency.

---

## 7. File and Directory Rules

### 7.1 Never Modify (Auto-generated / External)

| Path              | Reason                                    |
| ----------------- | ----------------------------------------- |
| `go.sum`          | Auto-generated by `go mod tidy`           |
| `data/`           | Runtime database files (gitignored)       |
| `omni` (binary)   | Build output (gitignored)                 |

### 7.2 Never Commit

| Path                  | Reason                         |
| --------------------- | ------------------------------ |
| `config.yaml`         | Contains secrets (API keys)    |
| `*.db`, `*.db-*`      | Runtime database files         |
| `memory_export.json`  | May contain private chat data  |
| `bot_memory.db`       | Legacy database file           |

### 7.3 Key Files to Know

| File                    | Purpose                                            |
| ----------------------- | -------------------------------------------------- |
| `main.go`               | Entry point — flag parsing, init, signal handling  |
| `config.yaml.example`   | Canonical config reference with all providers      |
| `.goreleaser.yaml`      | Cross-platform release config                      |
| `Dockerfile`            | Multi-stage production image                       |
| `.github/workflows/`    | CI (test/lint/build/docker) and Release pipelines  |

---

## 8. Dependency Management

- Use `go mod tidy` before committing any dependency change.
- Dependabot runs weekly for both `gomod` and `github-actions` ecosystems.
- Commit message prefix for dependency updates: `chore(deps)`.
- Pin major versions explicitly in `go.mod` `require` blocks.
- Do **not** vendor dependencies — this project uses module proxy.

---

## 9. Security Rules

> [!CAUTION]
> Violations of these rules can lead to credential exposure.

1. **Never commit `config.yaml`**. It contains bot tokens and API keys.
   Only `config.yaml.example` belongs in the repository.
2. **Never log API keys, bot tokens, or message content**. Log identifiers,
   sizes, model names, and token counts only.
3. **Never hardcode credentials**. All secrets come from `config.yaml` or
   environment variables.
4. **Never weaken `KnownFields(true)`** in the YAML parser. Strict parsing
   catches misconfigurations at startup.
5. **Sanitize all model output** before sending to Telegram. The
   `telegramhtml` package handles this — do not bypass it.
6. **Validate all numeric config bounds** in `config.validate()`. Do not
   allow negative token limits, out-of-range temperatures, or empty
   required fields to reach runtime.

---

## 10. Commit Conventions

This project uses [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>

- bullet point details
```

### Types

| Type       | When to Use                                            |
| ---------- | ------------------------------------------------------ |
| `feat`     | New feature or capability                              |
| `fix`      | Bug fix                                                |
| `refactor` | Code restructuring without behavior change             |
| `docs`     | Documentation only                                     |
| `test`     | Adding or updating tests                               |
| `chore`    | Build, CI, dependencies, tooling                       |
| `perf`     | Performance improvement                                |
| `style`    | Formatting, whitespace (no logic change)               |

### Scopes

Use the package name as scope: `bot`, `config`, `providers`, `storage`,
`telegramhtml`, `logging`, `update`, `version`, `ci`, `docker`, `deps`.

### Examples

```
feat(providers): add huggingface adapter

- implement OpenAI-compatible adapter for HF Inference API
- add default base URL and provider type constant
- add commented config example

fix(bot): handle empty caption in media group messages

chore(deps): bump google.golang.org/genai to v1.62.0
```

---

## 11. PR and Review Checklist

Before submitting changes, verify:

- [ ] `go build ./...` succeeds
- [ ] `go test -race ./...` passes
- [ ] `go vet ./...` reports no issues
- [ ] `golangci-lint run` passes
- [ ] `go mod tidy` leaves no diff
- [ ] No secrets, credentials, or prompt content in code or logs
- [ ] New provider adapters follow the embedding pattern in §5
- [ ] New config fields are validated in `config.validate()`
- [ ] New config fields have a commented example in `config.yaml.example`
- [ ] Existing comments and docstrings unrelated to the change are preserved
- [ ] Test coverage exists for new logic (table-driven, `testing` stdlib)

---

## 12. Environment Variables

| Variable         | Default  | Purpose                                  |
| ---------------- | -------- | ---------------------------------------- |
| `LOG_LEVEL`      | `info`   | `debug`, `info`, `warn`, `error`         |
| `LOG_FORMAT`     | `text`   | `text` or `json`                         |
| `LOG_ADD_SOURCE` | `false`  | Add source file/line to log entries      |

All other configuration is in `config.yaml`.

---

## 13. Docker

### Build

```bash
docker build \
  --build-arg VERSION=dev \
  --build-arg COMMIT=$(git rev-parse HEAD) \
  --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t omni .
```

### Run

```bash
docker run -v /path/to/config.yaml:/app/config.yaml omni
```

The Dockerfile produces a static `CGO_ENABLED=0` binary on Alpine. The
entrypoint is `/app/omni`.

---

## 14. Common Patterns Reference

### 14.1 Graceful Shutdown

```go
ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer cancel()
app.Run(ctx)
```

All long-running operations must respect context cancellation.

### 14.2 Error Sentinel and Type Assertions

Use `errors.Is` and `errors.As` for error inspection. The project defines
typed errors like `platforms.UnsupportedMediaError` — check with `errors.As`.

### 14.3 Nil-Safe Receivers

Registry methods (`Len()`, `DefaultModelID()`, `ProviderNames()`, etc.) are
nil-safe. Maintain this pattern for any new exported methods on pointer
receivers.

### 14.4 Table-Driven Config Validation

The `validate()` method in `config.go` uses indexed error messages
(`providers[%d].models[%d].name`). Follow this pattern when adding new
validation rules.
