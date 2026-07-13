# Omni

> A self-hosted Telegram AI assistant with multiple providers, persistent chat memory, streaming replies, image prompts, usage accounting, and native group mentions.

## 1: Overview

### 1.1: What Omni does

Omni connects a Telegram bot to Anthropic, Azure OpenAI, Cloudflare Workers AI, Cohere, DeepSeek, Google, Hugging Face, OpenAI, or another OpenAI-compatible chat completion API. It keeps each Telegram chat isolated in named sessions, remembers recent conversation history, streams model output back into Telegram, and stores operational state in SQLite, MySQL, or PostgreSQL.

Private conversations work like a normal direct message. In an allowed group, users start a message with the bot's username, such as `@your_bot_username Explain this code`, or reply to one of the bot's messages. Omni removes its own username before sending the prompt to the selected model.

### 1.2: Main features

- **Multiple AI providers:** Configure Amazon Bedrock, Anthropic, Azure OpenAI, Cloudflare Workers AI, Cohere, DeepSeek, Google, Groq, Hugging Face, Mistral, Ollama, OpenAI, Perplexity, Together AI, xAI (Grok), and custom OpenAI-compatible endpoints at the same time.
- **Conversation sessions:** Manage multiple isolated chat threads per Telegram conversation with automatic background title generation, configurable session timeouts, and an interactive inline-keyboard browser to switch between past contexts.
- **Reliable sender attribution:** Transparently resolves Telegram sender identities and `@mentions` in group chats to help the AI model distinguish between different participants.
- **Multimodal media:** Natively process images, audio, video, and voice notes (via Google Gemini) directly from Telegram.
- **Document analysis:** Read and analyze text from PDF, DOCX, XLSX, and source code files sent as attachments.
- **Per-model temperature:** Override the global temperature setting independently for each model in configuration.
- **Group media extraction:** Reply to an existing photo, audio, or video message with `@botname` in a group chat to instantly process it.
- **Unsupported-media protection:** Reject audio and video requests when the selected adapter cannot send that media instead of silently discarding attachments.
- **Per-chat model selection:** Use a two-column inline Telegram keyboard to choose a provider and model for each chat.
- **Streaming responses:** Receive a live preview while the model generates, followed by the complete response with dedicated error recovery and configurable HTTP timeouts per provider.
- **Persistent memory:** Store bounded conversation history in SQLite, MySQL, or PostgreSQL and restore it after a restart.
- **Context management:** Estimate tokens, reserve reply space, and drop the oldest history when the request approaches a model's context limit.
- **Usage tracking:** Record prompt, completion, and total token counts per user and chat.
- **Cost estimates:** Calculate approximate input and output costs from configured per-million-token prices.
- **Group privacy support:** Trigger the bot with a leading `@mention` while Telegram privacy mode remains enabled.
- **Access control:** Allow specific private users, administrators, and numeric group chat IDs.
- **Telegram-safe formatting:** Convert model Markdown to sanitized Telegram HTML and fall back to plain text when necessary.
- **Privacy-conscious logs:** Record identifiers and message size metrics without routinely logging prompt or response bodies.
- **Self-updating binary:** Run `omni update` to download and atomically replace the current binary from the latest GitHub release with checksum verification.
- **Container deployment:** Prebuilt Docker image published to GitHub Container Registry on every tagged release.
- **Context summarization:** Use `/summary` to generate a concise AI summary of recent conversation history on demand.

### 1.3: Supported interaction model

| Context | How to start an AI request |
| --- | --- |
| Allowed private chat | Send text or a photo directly |
| Allowed group | Start text or a photo caption with `@your_bot_username` |
| Allowed group reply | Reply to a message previously sent by the bot |
| Command | Use one of the registered management commands |

The legacy `/chat` command is not supported. Direct messages and native group mentions are the intended chat interface.

## 2: Requirements

### 2.1: Software

- Go `1.26.4` or a compatible newer toolchain.
- Network access to Telegram and every enabled AI provider.

### 2.2: Accounts and credentials

- A Telegram bot token created with [BotFather](https://t.me/BotFather).
- An API key for at least one enabled AI provider.
- Numeric Telegram user and group IDs for access control.

### 2.3: Telegram privacy mode

Telegram privacy mode can remain enabled. In groups, Telegram normally delivers commands, replies addressed to the bot, and messages that explicitly mention the bot. Omni's leading-mention router is designed for that behavior.

For predictable routing, place the bot username at the beginning of the message:

```text
@your_bot_username What is 2 + 2?
```

A mention later in the message is intentionally ignored by Omni's chat router.

## 3: Quick start

### 3.1: Prepare the configuration

From the repository root, copy the example and restrict access to the resulting file:

```sh
cp config.yaml.example config.yaml
chmod 600 config.yaml
```

Edit `config.yaml` and set at least these values:

- `providers[0].api_key`
- `telegram.bot_token`
- At least one ID under `telegram.allowed_user_ids`, `telegram.admin_user_ids`, or `telegram.allowed_group_ids`

Never commit `config.yaml`. It contains credentials and is ignored by the repository.

### 3.2: Build the bot

Download dependencies, run the test suite, and compile the binary:

```sh
go mod download
go test ./...
go build -o omni .
```

The default build creates `omni` in the repository root.

### 3.3: Run the bot

Place `config.yaml` beside the binary and start Omni:

```sh
./omni
```

Omni resolves the default configuration path relative to the executable, not the shell's current directory.

Use either flag to load a different configuration file:

```sh
./omni -c /absolute/path/to/config.yaml
./omni --config /absolute/path/to/config.yaml
```

The process handles `SIGINT` and `SIGTERM`, stops Telegram polling, and closes the database before exiting.

## 4: Configuration

### 4.1: Minimal configuration

This example enables one DeepSeek model and one private Telegram user:

```yaml
providers:
  - name: deepseek
    type: deepseek
    enabled: true
    api_key: "replace-with-provider-key"
    api_base: ""
    models:
      - name: deepseek-chat
        max_context_tokens: 65536
        input_price: 0.27
        output_price: 1.10

database:
  backend: "sqlite"
  sqlite:
    path: "omni.db"

global:
  initial_prompt: >-
    You are a helpful assistant. Use Telegram-compatible Markdown when
    formatting improves readability. Do not use LaTeX formatting.
  temperature: 1.3
  max_reply_tokens: 2048
  max_context_tokens: 8192
  history_size: 4
  sender_context: "groups"

telegram:
  bot_token: "replace-with-telegram-token"
  allowed_user_ids:
    - 123456789
  admin_user_ids: []
  allowed_group_ids: []
```

The parser rejects unknown fields, additional YAML documents, missing required values, duplicate provider names, and invalid numeric limits. This strict behavior catches obsolete options such as `groups` and `chat_command` during startup.

### 4.2: Provider configuration

Each item under `providers` defines one independently named backend.

| Field | Required | Meaning |
| --- | --- | --- |
| `name` | Yes | Unique provider name displayed in model selection |
| `type` | Recommended | `anthropic`, `bedrock`, `azure`, `cloudflare`, `cohere`, `deepseek`, `google`, `groq`, `huggingface`, `mistral`, `ollama`, `openai`, `perplexity`, `together`, `xai`, or `custom` |
| `enabled` | No | Enables the provider; defaults to `true` when omitted |
| `api_key` | Yes when enabled | Credential sent to the provider |
| `api_base` | No | Base endpoint; an empty value uses the type's default |
| `aws_region` | No | AWS Region for Bedrock (e.g. `us-east-1`); if omitted, falls back to environment |
| `aws_access_key` | No | AWS Access Key for Bedrock; if omitted, falls back to environment/IAM |
| `aws_secret_key` | No | AWS Secret Key for Bedrock; if omitted, falls back to environment/IAM |
| `api_version` | No | Azure OpenAI API version (e.g. `2024-02-15-preview`) |
| `cloudflare_account_id` | No | Cloudflare account ID for Workers AI |
| `timeout` | No | Per-provider HTTP timeout for connection and time-to-first-byte (e.g. `30s`, `1m`); defaults to no timeout |
| `models` | Yes when enabled | Models available through chat and `/model` |

If `type` is omitted, a provider named `anthropic`, `azure`, `bedrock`, `cloudflare`, `cohere`, `deepseek`, `google`, `groq`, `huggingface`, `mistral`, `ollama`, `openai`, `perplexity`, `together`, or `xai` inherits the matching type. Every other provider name defaults to `custom`.

Default base endpoints are:

| Provider type | Default base endpoint |
| --- | --- |
| `anthropic` | `https://api.anthropic.com` |
| `azure` | `https://%s.openai.azure.com` (formatted with `api_base` value) |
| `bedrock` | *(Uses AWS SDK regional endpoint)* |
| `cloudflare` | `https://api.cloudflare.com/client/v4/accounts/%s/ai/run` |
| `cohere` | `https://api.cohere.com` |
| `deepseek` | `https://api.deepseek.com` |
| `google` | `https://generativelanguage.googleapis.com/v1beta/openai/` |
| `groq` | `https://api.groq.com/openai/v1` |
| `huggingface` | `https://api-inference.huggingface.co/v1` |
| `mistral` | `https://api.mistral.ai/v1` |
| `ollama` | `http://localhost:11434/v1` |
| `openai` | `https://api.openai.com/v1` |
| `perplexity` | `https://api.perplexity.ai` |
| `together` | `https://api.together.xyz/v1` |
| `xai` | `https://api.x.ai/v1` |
| `custom` | `https://api.openai.com/v1` |

Disabled providers remain in the YAML file but are not loaded into the runtime registry or model menu. At least one provider must be enabled.

Anthropic uses its native Messages API and requires an Anthropic Console API key; it does not require a sign-in proxy. Its temperature range is `0` to `1`. When the global `global.temperature` is above `1`, every enabled Anthropic model must set a valid model-level `temperature` override or configuration validation will stop startup.

```yaml
- name: anthropic
  type: anthropic
  api_key: "replace-with-anthropic-api-key"
  models:
    - name: claude-sonnet-4-6
      temperature: 0.7
```

Azure OpenAI uses a resource-name as `api_base` and requires an `api_version`:

```yaml
- name: azure
  type: azure
  api_key: "your-azure-key"
  api_base: "my-resource-name"
  api_version: "2024-02-15-preview"
  models:
    - name: my-gpt-4-deployment
```

Cloudflare Workers AI requires a `cloudflare_account_id`:

```yaml
- name: cloudflare
  type: cloudflare
  api_key: "your-cf-api-token"
  cloudflare_account_id: "your-account-id"
  models:
    - name: "@cf/meta/llama-3-8b-instruct"
```

Cohere uses its native v2 API through the official Go SDK:

```yaml
- name: cohere
  type: cohere
  api_key: "your-cohere-key"
  models:
    - name: command-r-plus-08-2024
```

Hugging Face uses an OpenAI-compatible adapter:

```yaml
- name: huggingface
  type: huggingface
  api_key: "your-hf-token"
  models:
    - name: meta-llama/Llama-3.1-8B-Instruct
```

### 4.3: Model configuration

Each enabled provider must expose at least one model.

| Field | Required | Meaning |
| --- | --- | --- |
| `name` | Yes | Model identifier sent to the provider API |
| `input_price` | No | USD per one million prompt tokens |
| `output_price` | No | USD per one million completion tokens |
| `max_reply_tokens` | No | Model-specific reply limit; `0` inherits the global limit |
| `max_context_tokens` | No | Model-specific context limit; `0` inherits the global limit |
| `temperature` | No | Model-specific override; `0` to `2` generally and `0` to `1` for Anthropic |

The first model of the first enabled provider is the default. A selection made through `/model` is persisted per Telegram chat.

Pricing values are informational. Omni uses them for `/usage` estimates and does not perform billing.

### 4.4: Global configuration

| Field | Default | Validation and behavior |
| --- | --- | --- |
| `initial_prompt` | Empty | System message prepended to every model request |
| `temperature` | `1.3` | Must be between `0` and `2` |
| `max_reply_tokens` | `2048` | Must be greater than `0` |
| `max_context_tokens` | `8192` | Must be greater than `max_reply_tokens` |
| `history_size` | `4` | Maximum persisted history message entries; must be greater than `0` |
| `sender_context` | `"groups"` | Controls identity labels: `"groups"` (group chats only), `"all"`, or `"off"` |
| `session_timeout` | `"15m"` | Duration after which an idle session is automatically closed (e.g. `"10m"`, `"1h"`) |
| `max_sessions_displayed` | `10` | Maximum number of sessions shown per page in the `/conversation` menu |
| `title_model` | Empty | Dedicated fast model for background session title generation (e.g. `"gemini-2.5-flash"`); falls back to the current chat model when blank |
| `summary_prompt` | Built-in default | Custom prompt template for the `/summary` command; the default asks for a concise summary of recent messages |

Before a request is sent, Omni reserves `max_reply_tokens` inside the active context limit. It keeps the newest history entries that fit and drops older entries from the request. A model-level `max_context_tokens` overrides the global value for that model.

### 4.5: Database configuration

Omni uses a discriminated union pattern for database configurations. The `database.backend` field chooses which storage backend to load: `"sqlite"`, `"mysql"`, or `"postgres"`.

```yaml
database:
  backend: "sqlite" # "sqlite", "mysql", or "postgres"
  
  sqlite:
    path: "omni.db" # Required if backend is sqlite

  mysql:
    host: "127.0.0.1" # Required if backend is mysql
    port: 3306        # Required if backend is mysql
    user: "omni_user" # Required if backend is mysql
    password: "supersecretpassword"
    db_name: "omni"   # Required if backend is mysql

  postgres:
    host: "127.0.0.1" # Required if backend is postgres
    port: 5432         # Required if backend is postgres
    user: "omni_user"  # Required if backend is postgres
    password: "supersecretpassword"
    db_name: "omni"    # Required if backend is postgres
    sslmode: "require" # Optional: "disable", "require", "verify-ca", "verify-full"
```

For SQLite (`database.sqlite.path`), a relative path is resolved from the directory containing the loaded YAML file. The parent directory must already exist and be writable by the bot process.

MySQL and PostgreSQL connections are established at startup. Omni creates all required tables automatically for every supported backend. All three backends are covered by the same `storage.Store` interface, so switching backends does not affect bot behavior beyond the connection configuration.

> **Breaking Change:** Previous versions used `database.path` at the root of the database object. It is now properly scoped under `database.sqlite.path` via the `backend` selector.
### 4.6: Telegram access configuration

| Field | Meaning |
| --- | --- |
| `bot_token` | Telegram Bot API token |
| `allowed_user_ids` | Users allowed to interact in private chats |
| `admin_user_ids` | Startup-notification recipients who are also automatically allowed private users |
| `allowed_group_ids` | Numeric group or supergroup chat IDs allowed to use the bot |

Example:

```yaml
telegram:
  bot_token: "replace-with-telegram-token"
  allowed_user_ids:
    - 123456789
  admin_user_ids:
    - 987654321
  allowed_group_ids:
    - -1001234567890
```

Group authorization applies to the entire chat. Any member whose message reaches the bot can interact inside an allowed group. Per-user authorization is enforced for private messages, not for individual members of an allowed group.

### 4.7: Finding Telegram IDs

Send a message to the bot and inspect its structured logs. Incoming message metadata includes `chat_id`, `user_id`, message size, and chat type. Prompt and response bodies are excluded from routine logs.

For a group ID while privacy mode is enabled:

1. Add the bot to the group.
2. Send a command or a message beginning with the bot's username.
3. Find the negative `chat_id` in the log output.
4. Add that value to `telegram.allowed_group_ids`.
5. Restart Omni to reload the configuration.

## 5: Telegram usage

### 5.1: Private chats

An allowed user can send ordinary text directly:

```text
Summarize the differences between TCP and UDP.
```

No chat command or username prefix is required in a private conversation.

### 5.2: Group chats

In an allowed group, place the bot username first:

```text
@your_bot_username Summarize the last decision in this thread.
```

The username match is case-insensitive. Omni removes the matched mention and surrounding whitespace before calling the model. A longer, different username is not treated as a match.

When `sender_context` is enabled (the default for groups), Omni will automatically prepend `[telegram speaker: Name]` labels to user messages and format replies to preserve the flow of multi-user conversations.

Users can also reply directly to a message from the bot. The replied-to text is included as assistant context for the new request.

### 5.3: Photo prompts

Omni downloads the largest available Telegram photo variant and sends it as an image content part to the selected model.

- The maximum downloaded image size is 20 MiB.
- The detected media type must begin with `image/`.
- A photo without a caption receives the default prompt `What is in this image?`.
- In groups, start the photo caption with `@your_bot_username` unless the photo is sent as a reply to the bot.
- The selected provider and model must support the supplied multimodal request format.
- Anthropic accepts JPEG, PNG, GIF, and WebP photos through its native Messages API. Audio and video remain unsupported and are rejected before a request is sent.

Only a textual placeholder and optional caption are stored in conversation history; raw image bytes are not persisted in SQLite.

### 5.4: Document parsing

Omni extracts text from document attachments natively and adds it to the prompt context. Supported formats include PDF, DOCX, XLSX, and standard text or code files (e.g., `.go`, `.py`, `.md`).

- The maximum downloaded document size is 20 MiB.
- In groups, start the document caption with `@your_bot_username` or reply to the bot.
- The raw document is not saved to disk; text extraction happens entirely in memory.

### 5.5: Commands

| Command | Behavior |
| --- | --- |
| `/model` | Open an inline keyboard and persist the selected provider and model for the chat |
| `/ping` | Check the bot's network latency |
| `/version` | Display build and Go runtime information |
| `/clear` | Delete the current session's persisted and in-memory conversation history |
| `/new` | Start a fresh conversation session with a clean history |
| `/conversation` | Browse, switch to, or delete previously saved conversation sessions |
| `/summary` | Summarize the last N messages (default 20, max 100) using the current AI model |
| `/usage` | Show this user's token totals in the current chat and estimate cost when pricing is configured |
| `/setprompt` | Set a custom system prompt for the current chat |
| `/clearprompt` | Restore the configured default system prompt |
| `/export` | Export all stored conversations; restricted to explicitly allowed users and administrators |
| `/help` | Display the command summary |
| `/start` | Display the welcome message in a private chat |

The router also recognizes `!` as a command prefix. Telegram privacy mode may not deliver `!` commands from groups, so `/` is the reliable prefix there.

`/export` exports all stored chats, not only the current chat. The sender must be listed under `telegram.allowed_user_ids` or `telegram.admin_user_ids`, including when invoking the command from an allowed group.

### 5.6: Conversation sessions

Omni organizes conversation history into named sessions. Each private chat or group can have multiple independent sessions, each with its own bounded history and AI-generated title.

- A new Telegram chat automatically creates an initial session.
- Use `/new` to start a fresh session with a separate context window. The previous session is saved and can be resumed later.
- Use `/conversation` to browse the session list, switch to an earlier session, or delete unwanted sessions. The inline keyboard shows AI-generated titles and timestamps.
- Sessions that are idle beyond `global.session_timeout` are automatically closed. The next message in the chat starts a new session.
- Session titles are generated in the background by a dedicated lightweight model (`global.title_model`) when specified, or by the currently selected model as a fallback.
- The number of sessions shown per page in the browser is controlled by `global.max_sessions_displayed`.

Sessions are stored in the database alongside conversation history. The storage format is backend-agnostic: switching between SQLite, MySQL, or PostgreSQL preserves all sessions.

### 5.7: Streaming and long replies

Omni sends a typing action and periodically edits a preview message while tokens arrive. Preview edits are rate-limited to approximately one second in private chats and three seconds in groups.

Responses longer than the preview budget are sent in UTF-8-safe chunks. Omni prefers paragraph, newline, and word boundaries when splitting text.

### 5.8: Formatting behavior

Model output is rendered through a Telegram-safe HTML conversion layer. Raw HTML is sanitized, supported Markdown is converted, and malformed formatted sends fall back to plain text.

The example system prompt asks models for Telegram-compatible Markdown and discourages LaTeX because Telegram does not render mathematical notation natively.

## 6: Persistence and data

### 6.1: Database tables

Omni creates its schema automatically when the database opens on every supported backend.

| Table | Stored data |
| --- | --- |
| `sessions` | Active and archived conversation sessions with auto-generated titles and timestamps |
| `conversations` | Bounded JSON conversation history per session |
| `user_context` | Optional context data per chat |
| `token_usage` | Request-level prompt, completion, and total token counts per user and chat |
| `chat_models` | Persisted provider and model selection per chat |

Legacy `conversations` rows are automatically migrated into the `sessions` table when the database is first opened with a newer Omni version. The migration preserves existing history and creates a default session without data loss.

Conversation turns for the same session are serialized so concurrent updates cannot interleave their saved histories.

### 6.2: Memory export

The `/export` command writes `memory_export.json` in the process working directory with file mode `0600`. The export includes each known chat ID, stored messages, and optional context.

The database and export are not encrypted by Omni. Protect them with operating-system permissions, encrypted storage, and an appropriate backup policy.

### 6.3: Ignored runtime files

The repository ignores common local runtime artifacts, including:

- `config.yaml`
- `omni`
- `omni.db` and database sidecar files (SQLite)
- Databases under `data/`
- `memory_export.json`

## 7: Architecture

### 7.1: Package layout

```text
.
├── main.go
├── config.yaml.example
├── internal
│   ├── bot
│   ├── command
│   ├── config
│   ├── logging
│   ├── providers
│   │   └── platforms
│   ├── storage
│   ├── telegramhtml
│   ├── update
│   └── version
├── Dockerfile
├── go.mod
└── go.sum
```

| Package | Responsibility |
| --- | --- |
| `internal/config` | Strict YAML loading, defaults, normalization, and validation |
| `internal/bot` | Telegram update routing, commands, callbacks, streaming, images, and message delivery |
| `internal/command` | Independent command handlers for every Telegram command with shared bot context |
| `internal/providers` | Enabled-provider registry, model resolution, and provider adapter boundary |
| `internal/providers/platforms` | Native adapters for Anthropic, Cohere, and Google plus OpenAI-compatible adapters for Azure, Cloudflare, DeepSeek, Groq, Hugging Face, Mistral, Ollama, OpenAI, Perplexity, Together, and xAI |
| `internal/storage` | Multi-backend database layer (SQLite, MySQL, PostgreSQL) with session management, conversation history, model preferences, exports, and usage records |
| `internal/telegramhtml` | Markdown rendering and sanitized Telegram HTML output |
| `internal/update` | Self-update mechanism that fetches the latest GitHub release, verifies checksums, and atomically replaces the running binary |
| `internal/version` | Build-time version, commit, and Go runtime information |
| `internal/logging` | Structured application logging and safe text metrics |

### 7.2: Startup flow

1. Parse CLI flags (`--help`, `--version`, `--config`, `omni update`).
2. Configure the structured logger.
3. Resolve and validate the YAML configuration.
4. Build the registry from enabled providers.
5. Open the database (SQLite, MySQL, or PostgreSQL) and run migrations.
6. Initialize the Telegram client.
7. Call Telegram `getMe` and store the bot username for mention matching.
8. Register the command menu.
9. Notify configured administrators that the bot started.
10. Poll for updates until the process context is cancelled.

### 7.3: Message flow

1. Ignore updates without a supported text or photo message.
2. Check the private-user or group-chat allowlist.
3. Route recognized management commands.
4. Strip a valid leading bot mention when present.
5. Accept direct private messages or replies targeting the bot.
6. Resolve the active session (create a new one if the previous session timed out).
7. Load the chat's selected model and recent session history.
8. Prepare text or image content and enforce the context budget.
9. Stream the provider response into Telegram.
10. Persist the bounded history, session metadata, and any returned token usage.
11. Trigger asynchronous session title generation when needed.

## 8: CLI reference

### 8.1: Flags and subcommands

Omni accepts top-level flags before any subcommand:

```sh
omni --help            # Print usage information with OSC 8 hyperlinks
omni --version         # Print version, commit, build time, and Go runtime
omni --config /path/to/config.yaml  # Load a specific configuration file
omni -c /path/to/config.yaml        # Short form of --config
```

### 8.2: Self-update

The `update` subcommand downloads the latest GitHub release for the current platform and architecture, verifies its SHA-256 checksum, and atomically replaces the running binary:

```sh
omni update
```

The command exits with status 0 on success. On failure the original binary is left untouched. The update mechanism uses the GitHub Releases API and respects the repository's published checksum file.

## 9: Development

### 9.1: Test suite

Run every unit and integration-style package test:

```sh
go test ./...
```

The suite covers configuration validation, authorization helpers, mention parsing, token budgeting, UTF-8 message splitting, image prompt construction, provider behavior, multi-backend persistence, session management, self-update mechanics, safe error formatting, and Telegram HTML rendering.

### 9.2: Static checks

Run formatting and vet checks before submitting changes:

```sh
go fmt ./...
go vet ./...
```

`go fmt` rewrites Go source files, so run it only when the working tree contains changes you intend to format.

### 9.3: Focused package tests

```sh
go test ./internal/bot
go test ./internal/command
go test ./internal/config
go test ./internal/providers/...
go test ./internal/storage
go test ./internal/telegramhtml
go test ./internal/update
```

## 10: Operations

### 10.1: File placement

For a simple deployment, keep the executable and default configuration together:

```text
/opt/omni/
├── omni
├── config.yaml
└── data/
    └── omni.db
```

Set `database.sqlite.path` to `data/omni.db`, ensure `data/` exists, and grant the service account write access to the database directory.

### 10.2: Service example

A minimal systemd unit can run the bot under a dedicated account:

```ini
[Unit]
Description=Omni Telegram AI bot
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=omni
Group=omni
WorkingDirectory=/opt/omni
ExecStart=/opt/omni/omni --config /opt/omni/config.yaml
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

After creating the unit:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now omni
sudo systemctl status omni
```

### 10.3: Docker deployment

A prebuilt Docker image is published to GitHub Container Registry on every tagged release:

```sh
docker pull ghcr.io/andatoshiki/omni:latest
```

To build the image locally from the repository root:

```sh
docker build -t omni .
```

Run with a bind-mounted configuration:

```sh
docker run -d \
  --name omni \
  -v /opt/omni/config.yaml:/app/config.yaml \
  -v /opt/omni/data:/app/data \
  ghcr.io/andatoshiki/omni:latest
```

The image exposes no network ports. All communication is outbound to the Telegram Bot API and configured AI providers. Set `database.sqlite.path` to a path under `/app/data/` so the database persists across container restarts.

### 10.4: Backup guidance

- **SQLite:** Stop the bot or use a SQLite-aware backup method (`.backup` or `VACUUM INTO`) before copying an active database.
- **MySQL / PostgreSQL:** Use the database server's native backup tooling (`mysqldump`, `pg_dump`) or your cloud provider's managed backup feature.
- Protect backups because conversation history may contain sensitive content.
- Back up `config.yaml` through a secret-management system, not source control.
- Test database and configuration restoration before relying on a backup procedure.

## 11: Troubleshooting

### 11.1: Configuration file not found

The default path is `config.yaml` beside the executable. If the file lives elsewhere, pass `--config` with an absolute path.

### 11.2: Unknown YAML field

Omni intentionally rejects obsolete or misspelled keys. Remove legacy `groups` and `chat_command` sections. Configure groups only through `telegram.allowed_group_ids`.

### 11.3: Bot ignores a private message

- Confirm the sender's numeric ID is in `allowed_user_ids` or `admin_user_ids`.
- Restart the process after changing YAML.
- Inspect the warning log for `user not allowed`.

### 11.4: Bot ignores a group message

- Confirm the negative chat ID is in `allowed_group_ids`.
- Start the message with the exact bot username or reply to a bot message.
- Keep Telegram privacy mode behavior in mind: ordinary unmentioned group messages may never reach the process.
- Inspect the warning log for `group not allowed`.

### 11.5: Provider request fails

- Confirm the provider is enabled and has a non-empty API key.
- Verify `api_base` matches the provider's expected root endpoint.
- Confirm the configured model name exists for that account and endpoint.
- Check whether a custom provider supports streaming chat completions and usage metadata.

### 11.6: Prompt exceeds the context budget

Increase the applicable `max_context_tokens`, reduce `max_reply_tokens`, shorten the current prompt, or select a model with a larger configured context window. Omni can discard old history, but it cannot shrink the system prompt and current request below the reserved budget.

### 11.7: Database connection fails (MySQL / PostgreSQL)

- Confirm the host, port, user, password, and `db_name` are correct.
- For PostgreSQL, set `sslmode` to `require` when connecting to a managed cloud instance; use `disable` for local development.
- Ensure the database server is reachable from the bot's network.
- The database and user must already exist; Omni creates the tables automatically.

### 11.8: Session-related issues

- If sessions do not appear in `/conversation`, confirm the database backend is writable and the `sessions` table was created during startup.
- If titles are blank, verify the `title_model` is a valid model name accessible to an enabled provider. Use `"provider_name / model_name"` syntax to disambiguate when multiple providers expose the same model name.
- If sessions close too quickly, increase `global.session_timeout` (e.g. `"1h"` or `"4h"`).

### 11.9: Image prompt fails

- Confirm the image is no larger than 20 MiB after Telegram download.
- Select a model that accepts image content parts.
- Verify that Telegram returned a downloadable image file.
- Add a short caption to make the desired image task explicit.

## 12: Security

### 12.1: Credential handling

- Keep Telegram and provider tokens out of source control, logs, screenshots, and support messages.
- Store `config.yaml` with restrictive permissions such as `0600`.
- Rotate a token immediately if it is exposed.
- Use a dedicated service account with access only to required files and directories.

### 12.2: Access boundaries

- Private chats are authorized by Telegram user ID.
- Group chats are authorized by numeric chat ID and then shared by all members whose updates reach the bot.
- Administrators currently receive startup notifications and inherit private-user access.
- `/export` requires the sender to be explicitly listed under `allowed_user_ids` or `admin_user_ids`; group allowlisting alone is insufficient.
- Other management commands are not restricted to administrators.

### 12.3: Stored and logged data

The configured database backend stores session metadata, conversation history, model choices, and token usage. Routine structured logs omit prompt and response bodies but include identifiers, usernames, sizes, model names, timing, and token metrics.

Review storage, log retention, group membership, and export access according to the sensitivity of the conversations handled by your deployment.

## 13: License

### 13.1: GPL-3.0

Omni is distributed under the GNU General Public License version 3. See [LICENSE](LICENSE) for the complete terms.
