# gchatctl

[![CI](https://github.com/thomas-sievering/gchatctl/actions/workflows/ci.yml/badge.svg)](https://github.com/thomas-sievering/gchatctl/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/go-1.23%2B-00ADD8?logo=go)](https://go.dev/)
[![Release](https://img.shields.io/badge/release-not%20published-lightgrey)](https://github.com/thomas-sievering/gchatctl/releases)
[![Platforms](https://img.shields.io/badge/platforms-windows%20%7C%20linux%20%7C%20macOS-6f42c1)](#install)
[![License](https://img.shields.io/badge/license-MIT-green)](./LICENSE)

Google Chat CLI for agents.

Fast auth, read messages, and send messages.

## Quick Start

```powershell
# 1) One-time setup links (OAuth + Chat API)
gchatctl auth setup

# 2) Login (recommended scopes bundle)
gchatctl auth login --profile work --all-scopes

# 3) List your spaces
gchatctl chat spaces list --profile work --limit 20

# 4) Read last 10 DM messages with a person
gchatctl chat messages with --profile work --email user@company.com --limit 10

# 5) Send message
gchatctl chat messages send --profile work --email user@company.com --text "hello"
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
gchatctl auth login --profile work --all-scopes
gchatctl auth status --profile work --json
gchatctl auth logout --profile work
```

### Spaces

```powershell
gchatctl chat spaces list --profile work --limit 100
```

### Messages

```powershell
# Read by explicit space
gchatctl chat messages list --profile work --space spaces/AAA... --limit 20

# Send message by person or by space
gchatctl chat messages send --profile work --email user@company.com --text "hello"
gchatctl chat messages send --profile work --space spaces/AAA... --text "hello"

# Read DM history with a person
gchatctl chat messages with --profile work --email user@company.com --limit 10
```

## Files and Storage

`gchatctl` stores config/tokens in your user config dir:

- Windows config: `%APPDATA%\gchatctl\config.json`
- Windows tokens: `%APPDATA%\gchatctl\token_<profile>.json`

## Troubleshooting

- `insufficient auth scopes`:
  Run `gchatctl auth login --profile <profile> --all-scopes`
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
