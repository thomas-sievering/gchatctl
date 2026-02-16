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
  - Run: `gchatctl chat messages with --profile work --email simon@company.com --limit 10 --json`
- User says: "send hi to simon"
  - Run: `gchatctl chat messages send --profile work --email simon@company.com --text "hi"`
- User says: "list my spaces"
  - Run: `gchatctl chat spaces list --profile work --limit 100 --json`

## Steps

### 1) Validate auth first

Run:

```powershell
gchatctl auth status --profile work --json
```

If not authenticated:

```powershell
gchatctl auth login --profile work --all-scopes
```

### 2) Resolve destination explicitly

For person-based tasks use:

```powershell
gchatctl chat dm find --profile work --email user@company.com --json
```

Do not guess DM space IDs if email is known.

### 3) Read messages (compact JSON for agent parsing)

Preferred:

```powershell
gchatctl chat messages with --profile work --email user@company.com --limit 20 --json
```

Or by space:

```powershell
gchatctl chat messages list --profile work --space spaces/AAA... --limit 20 --json
```

### 4) Send messages safely

Always echo the outgoing text in the answer before sending.

```powershell
gchatctl chat messages send --profile work --email user@company.com --text "..."
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
  - `gchatctl auth login --profile work --all-scopes`
- If command returns `Google Chat app not found`, stop and instruct user to configure Chat app in Google Cloud.
- If user asks for "messages with <name>" but only a name is provided, ask for email or run lookup flow first.
- If sending fails, do not retry blindly; show exact API error and ask for confirmation before retry.
