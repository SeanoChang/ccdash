# llm-usage-dashboard — Phase 1 Design

**Date:** 2026-08-16
**Status:** Approved design, pending implementation plan
**Scope:** Phase 1 only — data ingestion + basic visualization

---

## 1. Purpose

A terminal tool that reads the transcripts Claude Code and Codex already write to
disk and reports how many tokens were spent, on what, and where.

It exists because two things are true of this machine:

1. **Most spend is invisible to existing tools.** 60% of Claude token usage comes
   from subagents and workflows, which live in nested transcript directories that
   main-loop-only readers skip.
2. **Claude history is being deleted.** `~/.claude/projects` holds ~22 days;
   `~/.codex/sessions` holds ~12 months. A cleanup process runs and stamps
   `~/.claude/.last-cleanup`. Any retrospective view that reads only live
   transcripts is permanently capped at the prune window.

The tool therefore ingests into a durable store that outlives its sources.

### In scope for Phase 1

- Ingest from both tools, handling every format hazard listed in §3.
- A SQLite archive whose rows survive deletion of the source files.
- Quota/limit ingestion from all three sources (§3.4), with provenance labels:
  Claude session, weekly-all-models, and per-model weekly (e.g. Fable); Codex
  5-hour and weekly.
- `llm-usage ingest` — headless, prints a summary; usable from cron/CI.
- `llm-usage setup-statusline` — opt-in, confirmed capture installation.
- `llm-usage` — a TUI with one screen (Overview): totals, a time series, a model
  breakdown, a per-project ranking, and a limits panel.

### Explicitly out of scope for Phase 1

Deferred, in build order: live Radar view with `fsnotify` tailing and burn-rate
projection; session and workflow forensics drill-down; the Archive management
view (coverage/gaps/prune/export); quota *history* charting (the samples are
captured in Phase 1, but only the latest state is rendered); `daemon --install`
launchd integration. Phase 1 must not foreclose these — §4's schema carries the
columns they need — but ships none of them.

---

## 2. Measured facts driving the design

Measured on this machine, snapshot **2026-08-16T16:33:51Z**.

**These figures move.** Active Claude sessions append transcripts continuously —
the request count rose from 11,585 to 11,599 and cost from $1,622.64 to $1,635.08
over forty minutes of design work. They are therefore *shape* constraints, not
frozen test constants; §9 defines how they are used in tests.

| Fact | Value | Stability |
|---|---|---|
| Claude transcript files | 463 | grows |
| — of which subagent transcripts | 412 (89%) | ratio stable |
| Codex rollout files | 248 | grows |
| Combined on-disk size | 385 MB | grows |
| Claude usage lines → unique requests | 25,329 → 11,599 | **2.18× duplication** |
| Codex `token_count` events | 18,228 | grows |
| — consecutive duplicates (no change) | 7,790 (**42.7%**) | ratio stable |
| Codex sessions, cumulative monotonic | 155 of 159 | 4 accumulator restarts |
| Full cold scan + parse (Python reference) | **0.95 s** | scales with corpus |
| Claude token total, deduped | 1,413.1 M | grows |
| — main loop / subagent (tokens) | 32.6% / 67.4% | **the load-bearing ratio** |
| — main loop / subagent (cost) | 40.3% / 59.7% | |
| Cache reads as share of input tokens | **96.1%** | |
| Fresh input tokens | 0.2 M of 1,413.1 M | |
| Claude cost at API list rates | $1,635.08 | grows |
| Unpriced models after normalization | `<synthetic>` only (18 rows) | |
| Rate-limit keys in Claude transcripts | **zero matches, all 463 files** | |
| Codex events with populated `secondary` | 17,297 | |
| Codex limit windows observed | 300 min (5 h) and 10080 min (7 d) | |
| `~/.claude.json` cache age when measured | **26 h 22 m** (its 5-hour reset already expired) | varies |

Two of these shape the design more than the totals do:

- **Subagents are two-thirds of token usage.** A main-loop-only reader measures
  the wrong thing on this workload.
- **96.1% of input tokens are cache reads**, and fresh input is 0.2 M against
  1,413.1 M total. Any cost model that ignores the ×0.10 cache-read multiplier
  overstates spend by roughly an order of magnitude. Cache accounting is not a
  refinement here; it is the dominant term.

`0.95 s` is a Python reference figure; Go with a byte-prefilter and bounded
goroutine fan-out is the floor, not the target.

---

## 3. Ingest

### 3.1 Source interface

Format knowledge is confined to `internal/source`. Everything downstream sees
only `model.Record`.

```go
type Source interface {
    Name() Tool                                  // claude | codex
    Discover(root string) ([]FileRef, error)     // files + stat for cursoring
    Parse(f FileRef, from int64) ([]model.Record, int64, error)
}
```

`Parse` resumes at byte offset `from` and returns the new offset. Transcript
files are append-only and become immutable when a session ends, so of ~700 files
exactly one is typically being written — freshness is a `stat()`, not a hash.

### 3.2 Claude adapter

Reads `~/.claude/projects/<project-slug>/**/*.jsonl`. Relevant records are
`type: "assistant"` with a `message.usage` object:

```json
{"type":"assistant","timestamp":"2026-08-15T23:58:43.829Z","sessionId":"f9e5…",
 "cwd":"/home/user/dev/projects/metrics/stocks/twn/data-cloud",
 "version":"2.1.233","model":"claude-fable-5","requestId":"req_011Ce5…",
 "usage":{"input_tokens":2,"cache_creation_input_tokens":21839,
          "cache_read_input_tokens":22003,"output_tokens":482,
          "output_tokens_details":{"thinking_tokens":384},
          "cache_creation":{"ephemeral_1h_input_tokens":21839,
                            "ephemeral_5m_input_tokens":0}}}
```

Rules:

- **Byte prefilter before decoding.** Skip any line not containing `"usage":{`.
  Two-thirds of lines fail this test, and the ones that fail are the fat ones
  (message text, tool output) — JSON decoding costs roughly 10–50× a substring
  scan, so the prefilter is where the speed comes from.
- **Deduplicate on `requestId`**, falling back to `message.id`. Streaming writes
  the same request up to 3×; skipping this inflates every total by 2.14×.
- **Cache write tiers are distinct.** `cache_creation.ephemeral_5m_input_tokens`
  and `ephemeral_1h_input_tokens` price at different multipliers (§5) and must
  be stored separately. Fall back to the flat `cache_creation_input_tokens` into
  the 5m bucket only when the split is absent.
- **Attribution from the path.** A path containing `/subagents/` is subagent
  work; `subagents/workflows/<wf_id>/agent-<id>.jsonl` yields workflow and agent
  IDs. `agent-<id>.meta.json` (a sibling file) supplies `spawnDepth` and the
  agent's model. These populate columns Phase 2 needs; Phase 1 only aggregates
  `agent != ''` as "subagent".

### 3.3 Codex adapter

Reads `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`. Relevant records are
`event_msg` payloads of `type: "token_count"`:

```json
{"type":"token_count",
 "info":{"total_token_usage":{"input_tokens":1221683,"cached_input_tokens":1088512,
                              "cache_write_input_tokens":0,"output_tokens":5834,
                              "reasoning_output_tokens":3776,"total_tokens":1227517},
         "last_token_usage":{…},"model_context_window":258400},
 "rate_limits":{"primary":{"used_percent":0.0,"window_minutes":10080,
                           "resets_at":1787404839},"plan_type":"plus"}}
```

**Codex reports cumulative counters, not deltas** — the opposite of Claude. The
canonical form is per-request deltas, so the adapter converts:

- **Diff consecutive `total_token_usage`, field by field, in file order.**
  Do *not* sum `last_token_usage`: it is repeated across events and over-counts
  by roughly 2× (measured: delta-sum exceeded the true total on 126 of 172
  files). Diffing is immune to this because a repeated event diffs to zero, and
  42.7% of events are exactly that.
- **Drop zero-delta records.** They carry no information.
- **A decrease is an accumulator restart, not corruption.** When a field's value
  drops below the previous one, emit the new value itself as the delta (the
  counter restarted from zero) and set `anomaly = 1` on that record. Measured on
  4 of 159 sessions; three restart at exactly 258,400, matching
  `model_context_window` — i.e. a context reset. Clamping to zero would silently
  lose that usage; assuming continuity would produce a negative. Flagging keeps
  the choice visible rather than burying it.
- **Normalize input semantics.** Codex's `input_tokens` *includes*
  `cached_input_tokens`; Claude's *excludes* cache reads. The canonical form is
  Claude's, so store `input = input_tokens - cached_input_tokens` and
  `cache_read = cached_input_tokens`. Skipping this makes Codex look ~10× heavier
  than it is.
- **Identity.** `session_meta` gives session ID, `cwd`, `cli_version`, and
  `originator` (`Codex Desktop` vs CLI). `turn_context` gives model and effort.
  The dedupe key is `<session-id>:<line-index-of-event>`, stable because files
  are append-only.

### 3.4 Limits — three sources, three provenances

Quota data is not in Claude transcripts at all (verified: zero matches for any
rate-limit key across all 463 files). It reaches disk by two other routes, and
Codex uses a third. Each carries different freshness, so **every displayed limit
is tagged with its provenance** — a percentage means something very different
when it is 26 hours old.

| Provenance | Source | Freshness | Covers |
|---|---|---|---|
| `live` | statusline JSONL capture (§3.4.1) | per render | Claude, only while a session runs |
| `cached` | `~/.claude.json` → `cachedUsageUtilization` | hours to days | Claude, incl. per-model scoped limits |
| `live` | Codex `token_count.rate_limits` | per turn | Codex 5-hour + weekly |

**Claude, cached.** `~/.claude.json` carries a `cachedUsageUtilization` object
with `fetchedAtMs` and a `utilization.limits[]` array. Measured live values:

```json
"limits": [
  {"kind":"session",       "group":"session","percent":16,"resets_at":"2026-08-15T15:19:59Z","is_active":false},
  {"kind":"weekly_all",    "group":"weekly", "percent":15,"resets_at":"2026-08-17T16:59:59Z","is_active":false},
  {"kind":"weekly_scoped", "group":"weekly", "percent":19,"resets_at":"2026-08-17T16:59:59Z","is_active":true,
   "scope":{"model":{"display_name":"Fable"}}}
]
```

This is a **cache, not a feed** — observed 26h22m stale, with a
`five_hour.resets_at` already in the past. It must never be shown without its
age. Sibling keys `seven_day_opus` / `seven_day_sonnet` exist and are null,
so the parser reads the `limits[]` array (which generalizes) rather than the
named fields.

**Codex, live.** Each `token_count` event carries `rate_limits.primary` and
`.secondary`, each with `used_percent`, `window_minutes`, and `resets_at`.
Measured: `window_minutes: 300` (5 hours) and `10080` (7 days), with a populated
`secondary` on 17,297 events. Window length identifies the limit kind — do not
assume primary is always the short window.

#### 3.4.1 Statusline capture

Claude Code invokes the configured statusline command once per render and writes
a JSON payload to its stdin containing `.rate_limits.five_hour` and
`.rate_limits.seven_day` (each with `used_percentage` and `resets_at`), plus
`.context_window`. This is the only live Claude quota channel; it is ephemeral
unless captured.

`llm-usage setup-statusline` appends a tee to the user's statusline script:

```sh
printf '%s\n' "$input" >> ~/.local/share/llm-usage-dashboard/statusline.jsonl
```

The command **shows the exact diff and requires confirmation before writing**,
backs up the original alongside it, and prints the snippet for manual addition
as an alternative. It never edits the script silently. If the file is absent or
the tee already present, it reports and exits without changes.

The captured file is append-only and read with the same cursor mechanism as
transcripts (§4.3).

#### 3.4.2 Change-detection storage

The statusline fires on every render and Codex emits `rate_limits` on every
turn, so storing each observation would produce enormous, mostly-redundant
tables. A sample is written **only when `percent` or `resets_at` differs from
the most recent sample for the same `(tool, kind, scope)`**. That yields a
compact time series with the transitions preserved and the flat stretches
collapsed — enough to chart quota burn without storing a row per keystroke.

---

## 4. Data model and storage

### 4.1 Record

```go
type Record struct {
    ID       string    // dedupe key: requestId | session:lineIndex
    Tool     Tool      // claude | codex
    TS       time.Time
    Model    string    // normalized (see §5)
    Project  string    // from cwd
    Session  string
    Agent    string    // "" for main loop
    Workflow string    // "" when not part of a workflow
    Depth    int       // spawnDepth; 0 for main loop

    InputTok     int64 // EXCLUDES cache reads — canonical
    OutputTok    int64
    ThinkingTok  int64
    CacheReadTok int64
    CacheWrite5m int64
    CacheWrite1h int64

    Anomaly bool // accumulator restart or other flagged irregularity
}
```

### 4.2 Schema

```sql
CREATE TABLE request (
  id TEXT PRIMARY KEY, tool TEXT NOT NULL, ts INTEGER NOT NULL,
  model TEXT NOT NULL, project TEXT, session TEXT,
  agent TEXT, workflow TEXT, depth INTEGER DEFAULT 0,
  in_tok INTEGER, out_tok INTEGER, think_tok INTEGER,
  cache_read INTEGER, cache_w5m INTEGER, cache_w1h INTEGER,
  anomaly INTEGER DEFAULT 0
);
CREATE INDEX request_ts      ON request(ts);
CREATE INDEX request_project ON request(project, ts);
CREATE INDEX request_model   ON request(model, ts);

-- ingest cursors; deliberately NOT foreign-keyed to request
CREATE TABLE source_file (
  path TEXT PRIMARY KEY, tool TEXT, size INTEGER,
  mtime INTEGER, offset INTEGER, last_seen INTEGER
);

-- models seen but not priced; surfaced in the UI, never silently dropped
CREATE TABLE unpriced (
  model TEXT PRIMARY KEY, count INTEGER, first_seen INTEGER, last_seen INTEGER
);

-- quota observations; one row per change, not per observation (§3.4.2)
CREATE TABLE limit_sample (
  tool TEXT NOT NULL,          -- claude | codex
  kind TEXT NOT NULL,          -- session | weekly_all | weekly_scoped
                               -- | codex_5h | codex_weekly
  scope TEXT NOT NULL DEFAULT '',  -- model display name; '' when unscoped
  percent REAL NOT NULL,
  resets_at INTEGER,           -- unix seconds; NULL when absent
  is_active INTEGER DEFAULT 0, -- 1 = the binding limit
  observed_at INTEGER NOT NULL,
  provenance TEXT NOT NULL,    -- live | cached
  PRIMARY KEY (tool, kind, scope, observed_at)
);
CREATE INDEX limit_latest ON limit_sample(tool, kind, scope, observed_at DESC);

CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);  -- schema_version, etc.
```

**The load-bearing rule:** `source_file` has no foreign key to `request`, and
removing a `source_file` row never deletes `request` rows. This is what makes the
store an archive rather than a cache — when Claude prunes a transcript, the rows
derived from it stay. Any future "clean up missing files" logic touches
`source_file` only.

Location: `~/.local/share/llm-usage-dashboard/usage.db` (honouring
`XDG_DATA_HOME`). Driver: `modernc.org/sqlite` (pure Go, preserves the
static-binary property; no CGO).

### 4.3 Ingest algorithm

```
for each discovered file:
    prior := source_file[path]
    if prior exists and size == prior.size and mtime == prior.mtime: skip
    from := prior.offset
    if size < prior.size: from = 0            // truncated/rewritten → full reparse
    records, newOffset := source.Parse(file, from)
    INSERT OR IGNORE each record              // PRIMARY KEY makes this idempotent
    upsert source_file{size, mtime, newOffset, last_seen}
```

`INSERT OR IGNORE` on a natural primary key makes re-ingest idempotent: running
`ingest` twice must not change any count. Fan-out is bounded by
`runtime.NumCPU()` — unbounded concurrent reads over 700 files saturate the
device queue and starve the process (observed during design work, where parallel
scans stalled unrelated commands for minutes).

---

## 5. Pricing

Rates are USD per million tokens, from the `claude-api` skill's table
(cached 2026-06-24):

| Model | Input | Output |
|---|---|---|
| `claude-fable-5`, `claude-mythos-5` | 10.00 | 50.00 |
| `claude-opus-5`, `claude-opus-4-8/4-7/4-6` | 5.00 | 25.00 |
| `claude-sonnet-5`, `claude-sonnet-4-6` | 3.00 | 15.00 |
| `claude-haiku-4-5` | 1.00 | 5.00 |

Multipliers on the input rate: **cache read ×0.10**, **cache write ×1.25 (5m
TTL)**, **cache write ×2.00 (1h TTL)**.

- **Model IDs must be normalized before lookup.** Claude Code emits both
  `claude-haiku-4-5` and `claude-haiku-4-5-20251001`; a naive table drops the
  dated form. Strip a trailing `-YYYYMMDD` and retry.
- **The table lives in `~/.config/llm-usage-dashboard/pricing.toml`**, written
  with defaults on first run and editable thereafter. Rates change, introductory
  pricing expires (Sonnet 5 is $2/$10 through 2026-08-31, $3/$15 after), and
  **Codex/OpenAI rates are not shipped** — they are the user's to supply.
  Codex rows show tokens and are excluded from cost totals until priced.
- **Unpriced models are recorded and displayed, never dropped.** Every row that
  cannot be priced increments `unpriced` and the UI shows a badge. A cost
  dashboard that quietly discards what it does not understand converges on
  confidently wrong totals.
- **Dollar figures are labelled "at API rates", never "spent".** These are
  subscription plans; the figure is what the usage *would* cost if metered. It
  is the right number for comparing projects and spotting waste, and the wrong
  number to call a bill.

---

## 6. Aggregation

`internal/agg` exposes plain SQL-backed queries; no ORM.

```go
func ByDay(db, filter) ([]DayBucket, error)      // date × tool × model
func ByModel(db, filter) ([]ModelBucket, error)
func ByProject(db, filter) ([]ProjectBucket, error)  // + 14-point sparkline
func Totals(db, filter) (Totals, error)              // tokens, cost, counts, range
func LatestLimits(db) ([]LimitState, error)          // newest sample per (tool,kind,scope)
```

`LimitState` carries `Tool`, `Kind`, `Scope`, `Percent`, `ResetsAt`, `IsActive`,
`ObservedAt`, and `Provenance`, plus a derived `Age` — the renderer needs the age
to decide whether to show a staleness warning (§3.4).

`Filter` carries a time range and optional tool/project/model. Each bucket
carries token fields and a computed cost; cost is derived at query time from the
pricing table, never stored — so editing `pricing.toml` re-prices history without
re-ingest.

---

## 7. TUI — the Overview screen

One screen in Phase 1. Rendering is text-cell only: braille (`⠀-⣿`, 2×4
subpixels per cell), block elements (`▁▂▃▄▅▆▇█`, `▏▎▍▌▋▊▉`), box drawing, and
24-bit color. **No Sixel, Kitty, or iTerm2 inline images** — this terminal runs
iTerm2 inside tmux 3.7b, which mangles all three; `COLORTERM=truecolor` confirms
the color path.

```
┌─ llm-usage ─────────────────────── 2026-07-25 → 08-16 · 11,599 req ─┐
│  1,413.1M tokens      $1,635.08 at API rates     cache read 96.1%   │
├──────────────────────────────────┬──────────────────────────────────┤
│  cost / day                      │  by model                        │
│   ⢀⠤⠒⠊⠉⠉⠒⠤⡀        ⣀⠤⠒⠉        │   fable-5    4,547  $907.76      │
│  ⡠⠊       ⠈⠑⢄  ⣀⠤⠒⠉             │   opus-5     6,402  $670.58      │
│  ─────────────────────────────   │   opus-4-8     135   $28.36      │
├──────────────────────────────────┴──────────────────────────────────┤
│  twn/data-cloud     ████████████████████  $601.00  ▃▅▂█▇█▅█         │
│  sample-project  ████████████          $378.19  ▂▃▂▂▁▃▂▂         │
│  ~/.claude          ███████               $236.94  ▁▂▁▃▂▁▂▃         │
├─ limits ────────────────────────────────────────────────────────────┤
│  claude  session   ▇▇░░░░░░░░  16%   resets 4h12m           live    │
│  claude  weekly    ▇▇░░░░░░░░  15%   resets 1d 4h           live    │
│  claude  Fable     ▇▇▇░░░░░░░  19%   resets 1d 4h  ◀ binding live   │
│  codex   5h        ▇░░░░░░░░░   8%   resets 2h30m           live    │
│  codex   weekly    ▇▇▇▇░░░░░░  34%   resets 3d              live    │
├─────────────────────────────────────────────────────────────────────┤
│  main 40.3% · subagent 59.7%          ⚠ 1 unpriced model   [q]uit   │
└─────────────────────────────────────────────────────────────────────┘
```

The limits panel renders one row per `(tool, kind, scope)` from
`agg.LatestLimits`. `◀ binding` marks `is_active`. The right column shows
provenance: `live` in normal weight, or `⚠ cached 26h` in a warning color when
the newest sample came from `~/.claude.json` (§3.4). A limit with no sample at
all renders as `— no data` rather than `0%`, since those are very different
statements.

Keys: `1/2/3` filter tool (all / claude / codex) · `d/w/m` range (day / week /
month) · `r` re-ingest · `q` quit. Layout reflows with `tea.WindowSizeMsg`;
below ~80 columns the panels stack vertically and the limits panel drops its
reset column before its bar.

Charts come from `ntcharts`; panels, borders, and color from `lipgloss`.

---

## 8. Error handling

| Condition | Behavior |
|---|---|
| Malformed JSON line | Skip that line, count it, continue the file |
| Unreadable file | Skip file, record path, continue; report count at end |
| Unknown model | Store the row, increment `unpriced`, show badge |
| Codex accumulator restart | Emit delta = new value, set `anomaly`, count it |
| Empty database | Overview renders an empty state prompting `llm-usage ingest` |
| Corrupt DB | Report the path and the command to remove it; never auto-delete |

The tool never deletes anything in `~/.claude` or `~/.codex`. It opens those
paths read-only.

---

## 9. Testing

Fixture-driven, with one fixture per hazard from §3 — each is a small `.jsonl`
checked into `testdata/`:

1. **Claude dedupe** — 3 records sharing a `requestId` collapse to 1.
2. **Codex delta conversion** — cumulative totals with a 40%+ duplicate rate
   produce deltas summing to the final total.
3. **Codex restart** — a decreasing counter yields delta = new value and
   `anomaly = 1`.
4. **Input normalization** — a Codex record whose `input_tokens` includes
   `cached_input_tokens` stores the difference.
5. **Cache tiers** — 5m and 1h writes price at ×1.25 and ×2.00.
6. **Dated model ID** — `claude-haiku-4-5-20251001` prices as `claude-haiku-4-5`.
7. **Unpriced model** — an unknown model stores the row and records it.
8. **Idempotent ingest** — running twice over the same fixture leaves row counts
   unchanged.
9. **Archive survival** — deleting a `source_file` row leaves `request` rows.
10. **Claude cached limits** — a `cachedUsageUtilization` fixture yields three
    rows (`session`, `weekly_all`, `weekly_scoped` scoped to `Fable`), each with
    `provenance = cached` and `observed_at` taken from `fetchedAtMs`.
11. **Statusline capture** — a statusline payload yields `session` and
    `weekly_all` rows with `provenance = live`.
12. **Codex window mapping** — `window_minutes: 300` maps to `codex_5h` and
    `10080` to `codex_weekly`, regardless of primary/secondary position.
13. **Change detection** — 50 identical consecutive observations insert one row;
    a changed `percent` inserts a second.

### 9.1 Differential test against a reference implementation

Frozen totals are the wrong acceptance criterion — the corpus grows while the
tool is being built (§2). Instead, the Python scripts written during design are
checked into `testdata/reference/` as an executable oracle, and CI runs a
differential check:

```
go run ./cmd/llm-usage ingest --json  >  got.json
python3 testdata/reference/snapshot.py --json  >  want.json
diff got.json want.json          # must be identical
```

Both read the same live corpus at the same moment, so drift cancels. The Go
implementation is correct when it agrees with the reference on every field:
unique request count, dedup ratio, per-model token splits, cache-tier splits,
main/subagent split, and total cost.

The reference implementation is deliberately naive — no cursors, no database,
no concurrency — so a disagreement points at the optimized path, which is where
bugs will be. When the two disagree, the reference is presumed right until
proven otherwise.

Fixture tests (§9's list) cover the hazards deterministically; the differential
test covers everything the fixtures did not anticipate.

---

## 10. CLI surface

```
llm-usage                     ingest, then open the TUI
llm-usage ingest              headless; prints the summary table; cron-safe
llm-usage ingest --full       ignore cursors, reparse everything
llm-usage limits              print current limits and exit (scriptable)
llm-usage setup-statusline    add the capture tee; shows a diff, asks first
llm-usage --db PATH           override store location
llm-usage version
```

`export` and `daemon` are Phase 2+.

---

## 11. Dependencies

Verified to resolve on 2026-08-16 against `proxy.golang.org`:

| Module | Version |
|---|---|
| `github.com/charmbracelet/bubbletea` | v1.3.10 |
| `github.com/charmbracelet/lipgloss` | v1.1.0 |
| `github.com/charmbracelet/bubbles` | v1.0.0 |
| `github.com/NimbleMarkets/ntcharts` | v0.5.1 |
| `modernc.org/sqlite` | v1.56.0 |

`github.com/fsnotify/fsnotify` (v1.10.1) is Phase 2.

---

## 12. Build sequence

1. `internal/model` — Record, LimitSample, model-ID normalization, pricing.
2. `internal/store` — schema, migrations, cursors, idempotent upsert,
   change-detection insert for limits.
3. `internal/source/claude` — transcript parser + fixtures 1, 5, 6, 7.
4. `internal/source/codex` — rollout parser + fixtures 2, 3, 4, 12.
5. `internal/source/limits` — `~/.claude.json` + statusline capture
   + fixtures 10, 11, 13.
6. `cmd/llm-usage ingest` + `limits` — wire it up; reproduce §2's numbers.
7. `cmd/llm-usage setup-statusline` — confirmed, backed-up tee installation.
8. `internal/agg` — the five queries.
9. `internal/render` + `internal/tui` — Overview screen incl. limits panel.

Step 6 is the milestone: at that point the tool is already useful headlessly, and
every later step is additive.

---

## 13. Assumptions and open questions

- **Claude's exact prune window is unconfirmed.** `cleanupPeriodDays` is absent
  from `settings.json`, so the built-in default applies; observed history spans
  22 days with usage gaps that blur the cutoff. The design does not depend on the
  exact number — only on history being bounded, which is established.
- **Codex pricing is user-supplied.** No OpenAI rates ship with the tool.
- **The 4 anomalous Codex sessions** are interpreted as context resets. Rows are
  flagged rather than corrected; if the interpretation proves wrong, only those
  rows change.
- **Project identity is the `cwd` path.** Moving or renaming a repository will
  appear as two projects. Aliasing is deferred.
