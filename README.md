# ClaudeScope

Local-first usage, cost, and pressure dashboard for Claude Code.

![ClaudeScope dashboard](assets/claudescope-dashboard.jpg)

ClaudeScope scans your local Claude Code session logs under `~/.claude/projects/` and turns token usage, model distribution, session/subagent rankings, cache behavior, estimated cost, and local pressure windows into a browser dashboard you can open directly.

It is a static HTML app: no backend, no account connection, no remote telemetry. Real usage data is written only to your local `data.js`, which is excluded by `.gitignore` by default.

[中文说明](README.zh-CN.md)

## Why

Claude Code can generate a large number of jsonl session logs across multiple workspaces. Raw logs make it hard to answer basic operational questions:

- How many tokens and how much estimated USD did I spend today / this week?
- Which workspace or session consumed the most?
- Which models were used most frequently?
- How much local pressure is building in the 5-hour and 7-day windows?
- How much did subagents actually consume, and which parent sessions do they belong to?

ClaudeScope computes those answers locally and keeps your data on your machine.

## What it is not

ClaudeScope is **not** an official Anthropic quota monitor. Anthropic does not expose account-level remaining quota through local Claude Code logs. The 5-hour / 7-day pressure cards are local estimates based on configurable token thresholds, intended as an early warning signal only. Always treat Anthropic Console as the source of truth for billing and account status.

## Features

- Token trends split by input, cache read, cache creation, and output tokens
- Time ranges: last 24 hours, today, 7 days, 30 days, all time, and custom ranges
- Session and model rankings with parent-session aggregation and optional subagent expansion
- Local pressure estimation for 5-hour and 7-day windows with explicit disclaimers
- Cost estimation based on public Anthropic USD prices
- Clear `unpriced` badges for unknown/custom models instead of silently showing `$0.00`
- Full override pricing via `~/.claude-scope/pricing.json`
- Custom pressure thresholds via `~/.claude-scope/risk.json`
- Automatic filtering for `<synthetic>` placeholder rows
- macOS and Windows release packages with prebuilt binaries

## Quick start

Download a release package:

- **macOS**: unzip `ClaudeScope-mac.zip`, then double-click `Open ClaudeScope.command`
- **Windows**: unzip `ClaudeScope-windows.zip`, then double-click `Open ClaudeScope.cmd`

The launcher runs the local generator, writes `data.js`, and opens `index.html` in your default browser. Running it again rescans changed Claude Code logs.

> If macOS blocks `Open ClaudeScope.command`, open **System Settings → Privacy & Security → Open Anyway**, or run `xattr -dr com.apple.quarantine .` in the extracted folder.

> GitHub's auto-generated **Source code (zip)** is for developers. End users should download `ClaudeScope-mac.zip` or `ClaudeScope-windows.zip`.

## CLI

```bash
claudescope generate [--root <dir>] [--out <file>] [--since <RFC3339>] [--window-days <n>]
claudescope open     [--out <file>]
claudescope version
```

From source:

```bash
npm install
npm run build
./bin/claude-scope generate
./bin/claude-scope open
```

## Configuration

Optional config files live in `~/.claude-scope/`:

| File | Purpose | Docs |
|------|---------|------|
| `pricing.json` | Override model pricing in USD per 1M tokens | [docs/pricing-config.md](docs/pricing-config.md) |
| `risk.json` | Customize local pressure window thresholds | [docs/risk-config.md](docs/risk-config.md) |

If a config file is missing, ClaudeScope uses built-in defaults. If a file is invalid, the generator prints a warning and falls back to defaults instead of breaking the dashboard.

## Data flow

1. Claude Code writes local session logs to `~/.claude/projects/<encoded-cwd>/<session-uuid>.jsonl`
2. Subagent logs are detected from nested `subagents/` paths
3. `claudescope generate` scans `*.jsonl` files and extracts message-level usage metadata only
4. Duplicate usage rows are globally deduplicated
5. Subagent tokens are merged into parent sessions by default while retaining expand/collapse metadata
6. Pricing and local pressure windows are computed from built-in or user-provided config
7. The result is written as `window.CLAUDESCOPE_DATA = {...}` in `data.js`
8. `index.html` loads sample data first, then real local `data.js` if present

## Privacy

ClaudeScope does not send data to any server. The generator reads usage metadata only:

- session id and workspace basename, not full paths
- model name and token counts
- timestamps, subagent parent relationship, and API failure status

It does **not** export conversation text, prompts, assistant replies, tool output, or file contents.

Review screenshots or `data.js` before sharing them.

## Cost estimation

Cost is an estimate: local token counts multiplied by public pricing rules. It is not an official bill. USD is the base unit. CNY display is a convenience conversion only and should not be used for billing.

## Schema

- [docs/schema-v3.md](docs/schema-v3.md) — V3 data format
- [docs/schema-v2-compat.md](docs/schema-v2-compat.md) — V2 compatibility path

## Build release packages

```bash
npm install
npm run release:local
```

Outputs:

- `dist/ClaudeScope-mac.zip`
- `dist/ClaudeScope-windows.zip`

## Developer verification

`npm run verify` uses Playwright for responsive layout snapshots. First-time setup requires browser binaries:

```bash
npm install
npx playwright install
npm run verify
```

End users do not need Playwright.

## Credits

ClaudeScope is forked from the community project [CodexScope](https://github.com/JUk1-GH/CodexScope), an OpenAI Codex usage dashboard. The static-dashboard architecture, `buildView` foundation, time utilities, worker-pool design, and release packaging flow originate from upstream.

This repository changes the data source to Claude Code and adds cache creation tokens, subagent parent aggregation, local pressure estimation, custom pricing/risk configuration, and Claude-specific schema handling.

Fork base:

- Upstream: <https://github.com/JUk1-GH/CodexScope>
- Upstream commit: `21fcc718de232cca7f3453f9156bf8fec1e2aae0` (2026-05-10)

Thanks to the CodexScope author for proving that a local-first, zero-backend, single-page usage dashboard is a great workflow.

## License

MIT
