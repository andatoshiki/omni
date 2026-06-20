# Omni

> A self-hosted Telegram AI assistant with multiple providers, persistent chat memory, streaming replies, image prompts, usage accounting, and native group mentions.

## 1: Overview

### 1.1: What Omni does

Omni connects a Telegram bot to DeepSeek, OpenAI, or another OpenAI-compatible chat completion API. It keeps each Telegram chat isolated, remembers recent conversation history, streams model output back into Telegram, and stores operational state in a local SQLite database.

Private conversations work like a normal direct message. In an allowed group, users start a message with the bot's username, such as `@your_bot_username Explain this code`, or reply to one of the bot's messages. Omni removes its own username before sending the prompt to the selected model.

### 1.2: Main features

- **Multiple AI providers:** Configure DeepSeek, OpenAI, and multiple custom OpenAI-compatible endpoints at the same time.
- **Per-chat model selection:** Use an inline Telegram keyboard to choose a provider and model for each chat.
- **Streaming responses:** Receive a live preview while the model generates, followed by the complete response.
- **Persistent memory:** Store bounded conversation history in SQLite and restore it after a restart.
- **Context management:** Estimate tokens, reserve reply space, and drop the oldest history when the request approaches a model's context limit.
- **Image prompts:** Send Telegram photos with optional captions to vision-capable models.
- **Usage tracking:** Record prompt, completion, and total token counts per user and chat.
- **Cost estimates:** Calculate approximate input and output costs from configured per-million-token prices.
- **Group privacy support:** Trigger the bot with a leading `@mention` while Telegram privacy mode remains enabled.
- **Access control:** Allow specific private users, administrators, and numeric group chat IDs.
- **Telegram-safe formatting:** Convert model Markdown to sanitized Telegram HTML and fall back to plain text when necessary.
- **Privacy-conscious logs:** Record identifiers and message size metrics without routinely logging prompt or response bodies.

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
- A C compiler supported by the Go toolchain because the SQLite driver uses CGO.
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
  path: "omni.db"

chat:
  initial_prompt: >-
    You are a helpful assistant. Use Telegram-compatible Markdown when
    formatting improves readability. Do not use LaTeX formatting.
  temperature: 1.3
  max_reply_tokens: 2048
  max_context_tokens: 8192
  history_size: 4

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
| `type` | Recommended | `deepseek`, `openai`, or `custom` |
| `enabled` | No | Enables the provider; defaults to `true` when omitted |
| `api_key` | Yes when enabled | Credential sent to the provider |
| `api_base` | No | Base endpoint; an empty value uses the type's default |
| `models` | Yes when enabled | Models available through `/model` |

If `type` is omitted, a provider named `deepseek` or `openai` inherits the matching type. Every other provider name defaults to `custom`.

Default base endpoints are:

| Provider type | Default base endpoint |
| --- | --- |
| `deepseek` | `https://api.deepseek.com` |
| `openai` | `https://api.openai.com/v1` |
| `custom` | `https://api.openai.com/v1` |

Disabled providers remain in the YAML file but are not loaded into the runtime registry or model menu. At least one provider must be enabled.

### 4.3: Model configuration

Each enabled provider must expose at least one model.

| Field | Required | Meaning |
| --- | --- | --- |
| `name` | Yes | Model identifier sent to the provider API |
| `input_price` | No | USD per one million prompt tokens |
| `output_price` | No | USD per one million completion tokens |
| `max_context_tokens` | No | Model-specific context limit; `0` inherits the global chat limit |

The first model of the first enabled provider is the default. A selection made through `/model` is persisted per Telegram chat.

Pricing values are informational. Omni uses them for `/usage` estimates and does not perform billing.

### 4.4: Chat configuration

| Field | Default | Validation and behavior |
| --- | --- | --- |
| `initial_prompt` | Empty | System message prepended to every model request |
| `temperature` | `1.3` | Must be between `0` and `2` |
| `max_reply_tokens` | `2048` | Must be greater than `0` |
| `max_context_tokens` | `8192` | Must be greater than `max_reply_tokens` |
| `history_size` | `4` | Maximum persisted history message entries; must be greater than `0` |

Before a request is sent, Omni reserves `max_reply_tokens` inside the active context limit. It keeps the newest history entries that fit and drops older entries from the request. A model-level `max_context_tokens` overrides the global value for that model.

### 4.5: Database configuration

`database.path` is required. A relative path is resolved from the directory containing the loaded YAML file.

```yaml
database:
  path: "data/omni.db"
```

The parent directory must already exist and be writable by the bot process.

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

Users can also reply directly to a message from the bot. The replied-to text is included as assistant context for the new request.

### 5.3: Photo prompts

Omni downloads the largest available Telegram photo variant and sends it as an image content part to the selected model.

- The maximum downloaded image size is 20 MiB.
- The detected media type must begin with `image/`.
- A photo without a caption receives the default prompt `What is in this image?`.
- In groups, start the photo caption with `@your_bot_username` unless the photo is sent as a reply to the bot.
- The selected provider and model must support the supplied multimodal request format.

Only a textual placeholder and optional caption are stored in conversation history; raw image bytes are not persisted in SQLite.

### 5.4: Commands

| Command | Aliases | Behavior |
| --- | --- | --- |
| `/model` | None | Open an inline keyboard and persist the selected provider and model for the chat |
| `/clear` | `/dsclear` | Delete the current chat's persisted and in-memory conversation history |
| `/usage` | `/dsusage` | Show this user's token totals in the current chat and estimate cost when pricing is configured |
| `/export` | `/dsexport` | Export all stored conversations to `memory_export.json` |
| `/help` | `/dshelp` | Display the command summary |
| `/start` | None | Display the welcome message in a private chat |

The router also recognizes `!` as a command prefix. Telegram privacy mode may not deliver `!` commands from groups, so `/` is the reliable prefix there.

`/export` is available to every allowed caller and exports all stored chats, not only the current chat. Treat access to the bot and its working directory accordingly.

### 5.5: Streaming and long replies

Omni sends a typing action and periodically edits a preview message while tokens arrive. Preview edits are rate-limited to approximately one second in private chats and three seconds in groups.

Responses longer than the preview budget are sent in UTF-8-safe chunks. Omni prefers paragraph, newline, and word boundaries when splitting text.

### 5.6: Formatting behavior

Model output is rendered through a Telegram-safe HTML conversion layer. Raw HTML is sanitized, supported Markdown is converted, and malformed formatted sends fall back to plain text.

The example system prompt asks models for Telegram-compatible Markdown and discourages LaTeX because Telegram does not render mathematical notation natively.

## 6: Persistence and data

### 6.1: SQLite tables

Omni creates its schema automatically when the database opens.

| Table | Stored data |
| --- | --- |
| `conversations` | Bounded JSON conversation history per chat |
| `user_context` | Optional context data per chat |
| `token_usage` | Request-level prompt, completion, and total token counts per user and chat |
| `chat_models` | Persisted provider and model selection per chat |

Conversation turns for the same chat are serialized so concurrent updates cannot interleave their saved histories.

### 6.2: Memory export

The `/export` command writes `memory_export.json` in the process working directory with file mode `0600`. The export includes each known chat ID, stored messages, and optional context.

The database and export are not encrypted by Omni. Protect them with operating-system permissions, encrypted storage, and an appropriate backup policy.

### 6.3: Ignored runtime files

The repository ignores common local runtime artifacts, including:

- `config.yaml`
- `omni`
- `omni.db` and database sidecar files
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
│   ├── config
│   ├── logging
│   ├── providers
│   │   └── platforms
│   ├── storage
│   └── telegramhtml
├── Dockerfile
├── go.mod
└── go.sum
```

| Package | Responsibility |
| --- | --- |
| `internal/config` | Strict YAML loading, defaults, normalization, and validation |
| `internal/bot` | Telegram update routing, commands, callbacks, streaming, images, and message delivery |
| `internal/providers` | Enabled-provider registry, model resolution, and provider adapter boundary |
| `internal/providers/platforms` | DeepSeek, OpenAI, and custom OpenAI-compatible HTTP adapters |
| `internal/storage` | SQLite schema, conversation history, model preferences, exports, and usage records |
| `internal/telegramhtml` | Markdown rendering and sanitized Telegram HTML output |
| `internal/logging` | Structured application logging and safe text metrics |

### 7.2: Startup flow

1. Configure the structured logger.
2. Resolve and validate the YAML configuration.
3. Build the registry from enabled providers.
4. Open SQLite and create missing tables.
5. Initialize the Telegram client.
6. Call Telegram `getMe` and store the bot username for mention matching.
7. Register the command menu.
8. Notify configured administrators that the bot started.
9. Poll for updates until the process context is cancelled.

### 7.3: Message flow

1. Ignore updates without a supported text or photo message.
2. Check the private-user or group-chat allowlist.
3. Route recognized management commands.
4. Strip a valid leading bot mention when present.
5. Accept direct private messages or replies targeting the bot.
6. Load the chat's selected model and recent history.
7. Prepare text or image content and enforce the context budget.
8. Stream the provider response into Telegram.
9. Persist the bounded history and any returned token usage.

## 8: Development

### 8.1: Test suite

Run every unit and integration-style package test:

```sh
go test ./...
```

The suite covers configuration validation, authorization helpers, mention parsing, token budgeting, UTF-8 message splitting, image prompt construction, provider behavior, SQLite persistence, safe error formatting, and Telegram HTML rendering.

### 8.2: Static checks

Run formatting and vet checks before submitting changes:

```sh
go fmt ./...
go vet ./...
```

`go fmt` rewrites Go source files, so run it only when the working tree contains changes you intend to format.

### 8.3: Focused package tests

```sh
go test ./internal/bot
go test ./internal/config
go test ./internal/providers/...
go test ./internal/storage
go test ./internal/telegramhtml
```

## 9: Operations

### 9.1: File placement

For a simple deployment, keep the executable and default configuration together:

```text
/opt/omni/
├── omni
├── config.yaml
└── data/
    └── omni.db
```

Set `database.path` to `data/omni.db`, ensure `data/` exists, and grant the service account write access to the database directory.

### 9.2: Service example

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

### 9.3: Backup guidance

- Stop the bot or use a SQLite-aware backup method before copying an active database.
- Protect backups because conversation history may contain sensitive content.
- Back up `config.yaml` through a secret-management system, not source control.
- Test database and configuration restoration before relying on a backup procedure.

## 10: Troubleshooting

### 10.1: Configuration file not found

The default path is `config.yaml` beside the executable. If the file lives elsewhere, pass `--config` with an absolute path.

### 10.2: Unknown YAML field

Omni intentionally rejects obsolete or misspelled keys. Remove legacy `groups` and `chat_command` sections. Configure groups only through `telegram.allowed_group_ids`.

### 10.3: Bot ignores a private message

- Confirm the sender's numeric ID is in `allowed_user_ids` or `admin_user_ids`.
- Restart the process after changing YAML.
- Inspect the warning log for `user not allowed`.

### 10.4: Bot ignores a group message

- Confirm the negative chat ID is in `allowed_group_ids`.
- Start the message with the exact bot username or reply to a bot message.
- Keep Telegram privacy mode behavior in mind: ordinary unmentioned group messages may never reach the process.
- Inspect the warning log for `group not allowed`.

### 10.5: Provider request fails

- Confirm the provider is enabled and has a non-empty API key.
- Verify `api_base` matches the provider's expected root endpoint.
- Confirm the configured model name exists for that account and endpoint.
- Check whether a custom provider supports streaming chat completions and usage metadata.

### 10.6: Prompt exceeds the context budget

Increase the applicable `max_context_tokens`, reduce `max_reply_tokens`, shorten the current prompt, or select a model with a larger configured context window. Omni can discard old history, but it cannot shrink the system prompt and current request below the reserved budget.

### 10.7: Image prompt fails

- Confirm the image is no larger than 20 MiB after Telegram download.
- Select a model that accepts image content parts.
- Verify that Telegram returned a downloadable image file.
- Add a short caption to make the desired image task explicit.

## 11: Security

### 11.1: Credential handling

- Keep Telegram and provider tokens out of source control, logs, screenshots, and support messages.
- Store `config.yaml` with restrictive permissions such as `0600`.
- Rotate a token immediately if it is exposed.
- Use a dedicated service account with access only to required files and directories.

### 11.2: Access boundaries

- Private chats are authorized by Telegram user ID.
- Group chats are authorized by numeric chat ID and then shared by all members whose updates reach the bot.
- Administrators currently receive startup notifications and inherit private-user access.
- Management commands are not otherwise restricted to administrators.

### 11.3: Stored and logged data

SQLite stores conversation history, model choices, and token usage. Routine structured logs omit prompt and response bodies but include identifiers, usernames, sizes, model names, timing, and token metrics.

Review storage, log retention, group membership, and export access according to the sensitivity of the conversations handled by your deployment.

## 12: License

### 12.1: GPL-3.0

Omni is distributed under the GNU General Public License version 3. See [LICENSE](LICENSE) for the complete terms.
