# ClaudeScope

[简体中文](README.md) | [English](README.en.md)

ClaudeScope is a local-first dashboard for understanding Claude Code usage from local session logs. It turns Claude Code metadata into a clean desktop dashboard with token trends, local pressure windows, session and subagent rankings, model distribution, cache behavior, and estimated cost.

![ClaudeScope dashboard](assets/claudescope-dashboard.jpg)

The dashboard is a static HTML app: no backend, no account connection, and no hosted telemetry. Your real usage export stays local in `data.js`, which is intentionally ignored by git.

## Why

Claude Code can generate a large number of jsonl logs across many workspaces and subagents. Raw logs are hard to reason about when you want to know where usage actually went.

ClaudeScope is built for that narrow job: generate a local export, open a local page, and see token volume, model mix, session hotspots, subagent cost, and local pressure signals in one place without shipping prompts or project data to another service.

## Features

- Cumulative token trend split by input, cache read, cache creation, and output tokens
- Date filters for last 24 hours, today, last 7 days, last 30 days, all history, and custom ranges
- Session and model rankings with token totals, request counts, and estimated cost
- Subagent-aware accounting: merge into parent sessions by default, expand on demand
- Local pressure estimation for 5-hour and 7-day windows with explicit disclaimers
- Preset switch: `--preset pro / max-5x / max-20x / custom` scales thresholds off a Pro baseline
- Overflow ratio: when usage exceeds the threshold, the UI shows `100% · 2.3×` instead of silently clamping
- Rate-limit hits: real Anthropic signals extracted from 429/529 error rows in your jsonl (7d/30d/all counts)
- Cost estimation by model and token type using public Anthropic USD prices
- Clear `unpriced` badges for unknown/custom models instead of silently showing `$0.00`
- User pricing overrides via `~/.claude-scope/pricing.json`
- User pressure thresholds + weights via `~/.claude-scope/risk.json`
- Automatic filtering for `<synthetic>` placeholder rows
- macOS and Windows release packages with prebuilt generators

## Quick Start

Normal users should download a platform package from [GitHub Releases](https://github.com/ArcanePivot/claudescope/releases):

- **macOS**: download `ClaudeScope-mac.zip`, unzip it, then double-click `Open ClaudeScope.command`
- **Windows**: download `ClaudeScope-windows.zip`, unzip it, then double-click `Open ClaudeScope.cmd`

Release zips include a prebuilt generator, so normal users do not need Go or Node.js. The launcher generates `data.js` from your local Claude Code logs and then opens `index.html` in your default browser.

> GitHub's automatic **Source code (zip)** asset is for developers, not the recommended user download. Prefer `ClaudeScope-mac.zip` / `ClaudeScope-windows.zip`.

If macOS blocks `Open ClaudeScope.command`, open **System Settings → Privacy & Security** and click **Open Anyway**. You can also run this once in Terminal from the extracted folder:

```bash
xattr -dr com.apple.quarantine .
```

## CLI

The release binary and the source-tree `bin/claude-scope` wrapper support:

```bash
claudescope generate [--root <dir>] [--out <file>] [--since <RFC3339>] [--window-days <n>] [--preset <pro|max-5x|max-20x|custom>]
claudescope open     [--out <file>]
claudescope version
```

`--preset` selects the local pressure preset. Priority: CLI flag > `risk.json` `preset` field > built-in `pro`.

From source:

```bash
npm install
npm run build
./bin/claude-scope generate
./bin/claude-scope open
```

By default, the generator reads Claude Code logs from:

```text
~/.claude/projects/
```

If your logs are stored elsewhere, pass the path explicitly:

```bash
claudescope generate --root /path/to/projects
```

## What Gets Displayed

- **Token trend**: cumulative input, cache read, cache creation, and output tokens over the selected range
- **Local pressure**: estimated 5-hour and 7-day pressure windows based on configurable token thresholds
- **Distribution**: request count or token volume grouped by time bucket
- **Rankings**: busiest sessions, subagents, and models for the selected period
- **Cost estimate**: local estimate by model and token type using pricing rules exported by the generator
- **Dataset notes**: scan count, dedup stats, filtered rows, pricing source, and schema version

## Configuration

Optional config files live in `~/.claude-scope/`:

| File | Purpose | Docs |
|------|---------|------|
| `pricing.json` | Override model pricing in USD per 1M tokens | [docs/pricing-config.md](docs/pricing-config.md) |
| `risk.json` | Customize local pressure window thresholds | [docs/risk-config.md](docs/risk-config.md) |

If a config file is missing, ClaudeScope uses built-in defaults. If a file is invalid, the generator prints a warning and falls back to defaults instead of breaking the dashboard.

## Data Flow

1. Claude Code writes local JSONL logs under `~/.claude/projects/<encoded-cwd>/`
2. Subagent logs are detected from nested `subagents/` paths
3. `claudescope generate` scans local `*.jsonl` files and extracts usage metadata only
4. Duplicate usage rows are globally deduplicated
5. Subagent tokens are merged into parent sessions by default while retaining expand/collapse metadata
6. Pricing and local pressure windows are computed from built-in or user-provided config
7. The generator writes `window.CLAUDESCOPE_DATA = {...}` to `data.js`
8. `index.html` loads bundled sample data first and then real local `data.js` if present

## Privacy

ClaudeScope does not send data to any server. The generator reads local Claude Code logs and exports only usage metadata:

- session id and working-directory basename, not full paths
- model name and token counts
- timestamps, subagent parent relationship, and API failure status

It does **not** export prompt text, assistant messages, tool output, file contents, or conversation transcripts.

Review screenshots or `data.js` before sharing artifacts generated from your own usage.

## Accuracy Notes

ClaudeScope is not an official Anthropic quota monitor. Anthropic does not expose account-level remaining quota through local Claude Code logs. The 5-hour / 7-day pressure cards are local estimates based on configurable token thresholds and should be treated as early warning signals only.

Cost is also an estimate: local token counts multiplied by public pricing rules. It is not an official bill. USD is the base unit; CNY display is a convenience conversion only and should not be used for billing.

## Schema

- [docs/schema-v3.md](docs/schema-v3.md) — V3 data format
- [docs/schema-v2-compat.md](docs/schema-v2-compat.md) — V2 compatibility path

## Build Release Packages

```bash
npm install
npm run release:local
```

Outputs:

- `dist/ClaudeScope-mac.zip`
- `dist/ClaudeScope-windows.zip`

The GitHub Actions release workflow builds the same zip files for tags named `v*`.

## Developer Verification

`npm run verify` uses Playwright for responsive layout snapshots. First-time setup requires browser binaries:

```bash
npm install
npx playwright install
npm run verify
```

End users do not need Playwright.

## Credits

ClaudeScope is forked from the community project [CodexScope](https://github.com/JUk1-GH/CodexScope), an OpenAI Codex usage dashboard. The static-dashboard architecture, `buildView` foundation, time utilities, worker-pool design, and release packaging flow originate from upstream.

This repository changes the data source to Claude Code and adds cache creation tokens, subagent parent aggregation, local pressure estimation, custom pricing/risk configuration, global usage-row deduplication, and Claude-specific schema handling.

Fork base:

- Upstream: <https://github.com/JUk1-GH/CodexScope>
- Upstream commit: `21fcc718de232cca7f3453f9156bf8fec1e2aae0` (2026-05-10)

Thanks to the CodexScope author for proving that a local-first, zero-backend, single-page usage dashboard is a great workflow.

## License

MIT
