# gchatctl

[![CI](https://github.com/thomas-sievering/gchatctl/actions/workflows/ci.yml/badge.svg)](https://github.com/thomas-sievering/gchatctl/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/go-1.23%2B-00ADD8?logo=go)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/thomas-sievering/gchatctl?display_name=tag)](https://github.com/thomas-sievering/gchatctl/releases)
[![Platforms](https://img.shields.io/badge/platforms-windows%20%7C%20linux%20%7C%20macOS-6f42c1)](#install)
[![License](https://img.shields.io/badge/license-MIT-green)](./LICENSE)

Google Chat CLI for agents.

Fast auth, read messages, and send messages.

## Quick Start

```powershell
# 1) Print setup links (copy/paste into any browser you choose)
gchatctl auth setup

# 2) Login (recommended scopes bundle)
gchatctl auth login --all-scopes

# 3) List your spaces
gchatctl chat spaces list --limit 20

# 4) Read what you received in last 15 minutes (all spaces)
gchatctl chat inbox --since 15m --limit 200

# 5) Read recent messages from a person (name lookup, no ID/email needed)
gchatctl chat recent --name "Simon" --limit 10

# 6) Send message
gchatctl chat send --email user@company.com --text "hello"
```

## Install

### Option A: Download Binary (recommended for users)

Use the GitHub Release asset for your OS and run `gchatctl` directly.

### Option B: Build from source (dev)

```powershell
go build -o gchatctl.exe .
```

End users do **not** need Go if you ship the binary.

## Commands

### Auth

```powershell
gchatctl auth setup
gchatctl auth login --all-scopes --json
gchatctl auth status --json
gchatctl auth logout
```

### Spaces

```powershell
gchatctl chat spaces list --limit 100
```

### Messages

```powershell
# Primary commands (recommended)
gchatctl chat inbox --since 15m --limit 200
gchatctl chat recent --name "Simon" --limit 10
gchatctl chat send --email user@company.com --text "hello"

# Read by explicit space (advanced)
gchatctl chat list --space spaces/AAA... --limit 20

# Full DM history with a person (includes both sides)
gchatctl chat with --name "Simon" --limit 20

# Send directly to a known space
gchatctl chat send --space spaces/AAA... --text "hello"

# Poll for new messages over time
gchatctl chat poll --since 5m --interval 30s --iterations 3 --json
```

## JSON Output

- `--json` now outputs compact JSON by default (agent-friendly).
- Set `GCHATCTL_JSON_PRETTY=1` to switch to pretty JSON for debugging.
- Auth commands support JSON too: `auth setup --json`, `auth login --json`, `auth status --json`.

```powershell
# compact JSON
gchatctl chat recent --name "Simon" --limit 10 --json

# pretty JSON
$env:GCHATCTL_JSON_PRETTY = "1"
gchatctl chat inbox --since 15m --limit 200 --json
```

## Files and Storage

`gchatctl` stores config/tokens in your user config dir:

- Windows config: `%APPDATA%\gchatctl\config.json`
- Windows token: `%APPDATA%\gchatctl\token.json`

## Troubleshooting

- `insufficient auth scopes`:
  Run `gchatctl auth login --all-scopes`
- `Google Chat app not found`:
  Enable Chat API and complete Chat app configuration in Google Cloud.

## Automated Releases

This repo includes `.github/workflows/release.yml`.

On tag push (`v*`), GitHub Actions will:

- Build binaries for Windows/Linux/macOS (amd64 + arm64)
- Package assets (`.zip` for Windows, `.tar.gz` for Linux/macOS)
- Publish them to the GitHub Release for that tag

Publish a release:

```powershell
git tag v0.1.0
git push origin v0.1.0
```
