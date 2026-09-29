# 🦉 Omni

> A self-hosted Telegram AI assistant with multiple providers, persistent chat memory, streaming replies, image prompts, usage accounting, and native group mentions.

Read the complete documentation in the [Omni Wiki](https://github.com/andatoshiki/omni/wiki).

In an allowed group, start a message or sentence with the case-insensitive wake word `omni` to talk to the bot without typing its `@username` (for example, `omni`, `Omni`, and `OMNI` all work). The bot must be a group administrator or have Group Privacy disabled through BotFather's `/setprivacy` setting so Telegram delivers ordinary group messages to it.

## User access

Configure exactly one administrator by numeric Telegram user ID or quoted personal username:

```yaml
telegram:
  bot_token: "..."
  admin_user: "@andatoshiki"
  allowed_group_ids: []
```

The administrator can manage private-chat access without editing the configuration:

```text
/addusr @username
/addusr 123456789
/delusr @username
/delusr 123456789
```

The administrator can also reply to a user's message with `/addusr` or `/delusr`; the immutable sender ID from the replied message is used. An explicit username or ID takes precedence when supplied.

Numeric IDs work immediately. A username works after the bot has observed that user in a private or allowed-group update; otherwise, ask the user to message the bot or use their numeric ID. A configured admin handle is resolved from those observations on each startup, or bootstrapped by the first matching update when it has not been seen yet. Because Telegram usernames can be transferred, a numeric `admin_user` is the safest production setting; if a configured handle changes owners, the bot deliberately keeps the current administrator for the running process and re-resolves the handle after restart. User access is stored in the configured database and survives restarts. The former `allowed_user_ids` and `admin_user_ids` settings are no longer supported.

Developed with ❤️ by [Anda Toshiki](https://toshiki.dev) at the innovative research lab of [ASU](https://asu.edu).
