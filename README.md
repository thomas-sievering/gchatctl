# gchatctl

[![CI](https://github.com/thomas-sievering/gchatctl/actions/workflows/ci.yml/badge.svg)](https://github.com/thomas-sievering/gchatctl/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/go-1.23%2B-00ADD8?logo=go)](https://go.dev/)
[![Release](https://img.shields.io/badge/release-not%20published-lightgrey)](https://github.com/thomas-sievering/gchatctl/releases)
[![Platforms](https://img.shields.io/badge/platforms-windows%20%7C%20linux%20%7C%20macOS-6f42c1)](#install)
[![License](https://img.shields.io/badge/license-MIT-green)](./LICENSE)

Google Chat CLI for agents.

Fast auth, direct-message lookup by email, read/send, polling, and unread checks.

> If your repo name differs from `gchatctl`, update badge links accordingly.

## Quick Start

```powershell
# 1) One-time setup links (OAuth + Chat API)
gchatctl auth setup

# 2) Login (recommended scopes bundle)
gchatctl auth login --profile work --all-scopes

# 3) Find DM space with a person by email
gchatctl chat dm find --profile work --email simon.hartstein@bmtg.ch

# 4) Read last 10 messages (both sides)
gchatctl chat messages with --profile work --email simon.hartstein@bmtg.ch --limit 10

# 5) Send message
gchatctl chat messages send --profile work --email simon.hartstein@bmtg.ch --text "yoyo"
```

## Install

### Option A: Download Binary (recommended for users)

Use the GitHub Release asset for your OS and run `gchatctl` directly.

### Option B: Build from source (dev)

```powershell
go build -o gchatctl.exe .
```

End users do **not** need Go if you ship the binary.

## Core Commands

### Auth

```powershell
gchatctl auth setup
gchatctl auth login --profile work --all-scopes
gchatctl auth status --profile work --json
gchatctl auth logout --profile work
```

### DM / Messages

```powershell
# Resolve a direct-message space by person
gchatctl chat dm find --profile work --email user@company.com

# Read combined DM log (you + other person)
gchatctl chat messages with --profile work --email user@company.com --limit 10

# Read by explicit space
gchatctl chat messages list --profile work --space spaces/AAA... --limit 20

# Send message by person or by space
gchatctl chat messages send --profile work --email user@company.com --text "hello"
gchatctl chat messages send --profile work --space spaces/AAA... --text "hello"
```

### Monitoring

```powershell
# Native unread (requires read-state scope)
gchatctl chat spaces unread --profile work --json

# Poll new messages in last window
gchatctl chat messages poll --profile work --space spaces/AAA... --since 5m --interval 30s --iterations 10
```

### User Aliases (when API hides display names)

```powershell
gchatctl chat users aliases set --user users/123... --name "Simon"
gchatctl chat users aliases list
gchatctl chat users aliases unset --user users/123...
```

## Files and Storage

`gchatctl` stores config/tokens in your user config dir:

- Windows config: `%APPDATA%\gchatctl\config.json`
- Windows tokens: `%APPDATA%\gchatctl\token_<profile>.json`
- Aliases: `%APPDATA%\gchatctl\aliases.json`

## Troubleshooting

- `insufficient auth scopes`:
  Run `gchatctl auth login --profile <profile> --all-scopes`
- `Google Chat app not found`:
  Enable Chat API and complete Chat app configuration in Google Cloud.
- Missing names in DMs:
  Use `chat dm find --email ...` and aliases for stable identity in output.

## Release Notes for GitHub Push

Recommended release layout:

- Attach per-OS binaries to GitHub Releases
- Keep README examples binary-first (`gchatctl ...`)
- Keep source build as secondary path

Example cross-build commands:

```powershell
# Windows
go build -o dist/gchatctl-windows-amd64.exe .

# Linux/macOS (run in CI or matching env)
# GOOS=linux GOARCH=amd64 go build -o dist/gchatctl-linux-amd64 .
# GOOS=darwin GOARCH=arm64 go build -o dist/gchatctl-darwin-arm64 .
```
