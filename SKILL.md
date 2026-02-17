---
name: gchatctl
version: "1.0"
description: Use this skill when user asks to read or send Google Chat messages from terminal, especially "messages with <person>", "send to <person>", "last messages", or "check spaces".
user-invocable: true
argument-hint: "[read|send|spaces|auth] [target] [options]"
allowed-tools: Read, Bash
---

# gchatctl Skill

Agent workflow for using `gchatctl` safely and quickly.

## Arguments

Parse `$ARGUMENTS` into:
- `mode`: `read`, `send`, `spaces`, or `auth` (first token if present)
- `target`: person email, `users/...`, `spaces/...`, or free text
- `extra`: remaining flags or message text

If mode is missing, infer from user request:
- "last messages", "history", "what did X say" -> `read`
- "send", "write to", "message X" -> `send`
- "spaces", "where can I chat" -> `spaces`
- "login", "auth", "token" -> `auth`

## Examples

- User says: "give me last 10 messages with simon"
  - Run: `gchatctl chat recent --name "Simon" --limit 10 --json`
- User says: "send hi to simon"
  - Run: `gchatctl chat send --email simon@company.com --text "hi"`
- User says: "list my spaces"
  - Run: `gchatctl chat spaces list --limit 100 --json`

## Steps

### 1) Validate auth first

Run:

```powershell
gchatctl auth status --json
```

If not authenticated:

```powershell
gchatctl auth setup
gchatctl auth login --all-scopes --json
```

### 2) Resolve destination with built-in name lookup

For person-based reads, prefer:

```powershell
gchatctl chat recent --name "Simon" --limit 20 --json
```

Use `chat recent` when user asks what a person said (messages from that sender).
Use `chat with` when user asks for full DM history (both participants).
If exact identity is required, use `--email` or `--user`.

### 3) Read messages (compact JSON for agent parsing)

Preferred:

```powershell
gchatctl chat recent --name "Simon" --limit 20 --json
```

Or by space:

```powershell
gchatctl chat list --space spaces/AAA... --limit 20 --json
```

### 4) Send messages safely

Always echo the outgoing text in the answer before sending.

```powershell
gchatctl chat send --email user@company.com --text "..."
```

After send, report destination space and message ID from command output.

### 5) Return structured results

When user asks for analysis, parse JSON and summarize:
- last timestamp
- sender
- message snippet
- message count

Use exact timestamps in UTC when reporting "latest".

## Error Handling

- If command returns `insufficient auth scopes`, run:
  - `gchatctl auth login --all-scopes`
- If command returns `Google Chat app not found`, stop and instruct user to configure Chat app in Google Cloud.
- If user asks for "messages with <name>" and only a name is provided, use `chat recent --name "<name>"` first.
- Prefer explicit aliases (`chat users aliases set` / `set-from-space`) over `aliases infer`; treat inference as fallback only.
- If sending fails, do not retry blindly; show exact API error and ask for confirmation before retry.

## Setup Notes

- If user asks for browser choice during setup, use `gchatctl auth setup` and have them open the printed links manually.
