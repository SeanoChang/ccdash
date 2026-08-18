# ccdash

A terminal dashboard for what your AI coding agents actually cost.

ccdash reads the transcripts Claude Code and Codex already write to your disk,
archives them into a local SQLite database, and shows you tokens, cost, and
quota usage — by project, session, model, day, subagent, or workflow.

Everything runs locally. ccdash makes no network requests, reads only files the
agents already wrote, and never sends your data anywhere.

<!-- SCREENSHOT: hero — the Projects view at ~120x35, showing the header with
     range and totals, a few projects with cost bars and trend sparklines.
     Save as docs/images/hero.png, then delete this comment and uncomment: -->
<!-- ![ccdash](docs/images/hero.png) -->

## Why

The transcripts on your disk contain every token you have spent, but they are
JSONL files scattered across two directories and they get pruned on a rolling
window. ccdash turns them into a durable archive you can ask questions of:

- Which project is burning the most money, and is it trending up?
- How much of my spend is subagents rather than the main loop?
- What is my 5-hour limit at right now, and when does the weekly reset?
- Did that one workflow really cost what I think it did?

Cost is computed at query time from an editable rate table, never stored — so
correcting a price re-prices your entire history with no re-ingest.

## Install

Requires Go 1.26 or newer.

```bash
go install github.com/SeanoChang/ccdash/cmd/ccdash@latest
```

Or build from a clone:

```bash
git clone https://github.com/SeanoChang/ccdash
cd ccdash
go build -o ccdash ./cmd/ccdash
```

## Usage

```bash
ccdash                      # ingest, then open the dashboard
ccdash ingest               # ingest only, print a summary
ccdash ingest --full        # re-parse every source file from the start
ccdash ingest --json        # machine-readable totals, models, unpriced
ccdash limits               # print current quota state and exit
ccdash setup-statusline     # capture live Claude limits (see below)
ccdash version
```

`--db PATH` points at a different archive. The default lives at
`$XDG_DATA_HOME/ccdash/usage.db`, falling back to
`~/.local/share/ccdash/usage.db`.

### Views

Press `:` and type a name, or its prefix:

| View        | Shows                                              |
| ----------- | -------------------------------------------------- |
| `projects`  | Cost per project with a 14-day trend sparkline     |
| `sessions`  | Sessions newest first, with duration and cost      |
| `requests`  | Individual requests, paginated                     |
| `agents`    | Subagent attribution — which agent, which workflow |
| `workflows` | Whole workflows, with distinct agent counts        |
| `models`    | Token and cost breakdown per model                 |
| `days`      | Daily totals                                       |
| `limits`    | Quota state per tool, with provenance and age      |
| `pulse`     | Cost over time as a braille chart                  |
| `unpriced`  | Models the rate table cannot price                 |

<!-- SCREENSHOT: sessions or requests view, drilled in from a project, so the
     breadcrumb at the bottom shows the stack. Save as docs/images/drill.png,
     then delete this comment and uncomment: -->
<!-- ![Drilling from a project into its sessions](docs/images/drill.png) -->

<!-- SCREENSHOT: the pulse view — the braille cost chart carries the project
     better than any table does. Save as docs/images/pulse.png, then delete
     this comment and uncomment: -->
<!-- ![Cost over time](docs/images/pulse.png) -->

### Keys

| Key                 | Does                                            |
| ------------------- | ----------------------------------------------- |
| `j` `k` `↓` `↑`     | Move selection                                  |
| `ctrl-f` `ctrl-b`   | Page down / up                                  |
| `g` `G`             | First / last row                                |
| `enter`             | Drill into the selected row                     |
| `esc`               | Pop back one level                              |
| `s` `S`             | Change sort column / reverse direction          |
| `/`                 | Filter the current table                        |
| `:`                 | Command prompt                                  |
| `r`                 | Refresh now                                     |
| `1` `2` `3`         | Tool filter: all / claude / codex               |
| `d` `w` `m` `a`     | Rolling range: 24h / 7d / 30d / all             |
| `D` `W` `M`         | Calendar range: today / this week / this month  |
| `?`                 | Help                                            |
| `q` `ctrl-c`        | Quit                                            |

## Where the data comes from

| Source                                | Provides                          |
| ------------------------------------- | --------------------------------- |
| `~/.claude/projects/**/*.jsonl`       | Claude Code requests and usage    |
| `~/.codex/sessions/**/*.jsonl`        | Codex requests, usage, and limits |
| `~/.claude.json`                      | Cached Claude quota utilization   |
| `~/.local/share/ccdash/statusline.jsonl` | Live Claude limits, if enabled |

The archive outlives its sources. Claude prunes transcripts on a rolling
window, so ccdash keeps request rows indefinitely and never deletes them when a
source file disappears.

### Live Claude limits

Claude Code's 5-hour and weekly limits are only visible in the payload it pipes
to your statusline script. `ccdash setup-statusline` appends a one-line capture
to `~/.claude/statusline-command.sh`. It shows you the exact diff, asks for
confirmation, and writes a backup first — it is the only command that ever
writes outside ccdash's own directories.

Without it, ccdash falls back to the cached utilization in `~/.claude.json`,
which can be stale or absent. Limits with no live sample render as an empty
gauge with a reason, never as a stale number.

<!-- SCREENSHOT: the limits view, ideally with at least one live gauge and one
     stale or absent one, so the provenance and age columns are visible. That
     contrast is the point of the panel. Save as docs/images/limits.png, then
     delete this comment and uncomment: -->
<!-- ![Quota limits with provenance and age](docs/images/limits.png) -->

## Screenshots

<!-- Drop any additional PNGs here. Suggested, in rough order of usefulness to
     someone deciding whether to install:
       docs/images/models.png    cost split across models
       docs/images/agents.png    subagent attribution
       docs/images/help.png      the ? keymap
     Reference them as: ![description](docs/images/name.png) -->

_Screenshots coming._

## Pricing

Rates live in `$XDG_CONFIG_HOME/ccdash/pricing.toml` (default
`~/.config/ccdash/pricing.toml`), written on first run with published rates for
the models ccdash knows.

Models with no published rate are deliberately left unpriced rather than
guessed. Their tokens are still counted and their rows still appear; the cost
column shows an em dash instead of a misleading `$0.00`, and the `unpriced`
view lists them. Add a rate to the file and the entire history re-prices on the
next refresh.

## Status

Early. The archive format and the CLI are stable enough to use daily; the TUI
is still growing. See `docs/superpowers/specs/` for the design documents and
`docs/superpowers/plans/` for what is being built next.

## License

MIT — see [LICENSE](LICENSE).
