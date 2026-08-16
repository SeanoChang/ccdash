# Reference implementation

Deliberately naive Python readers written during design (2026-08-16). They are
the oracle for the Go implementation's differential test (spec §9.1): no
cursors, no database, no concurrency, so a disagreement points at the optimized
path rather than at the parsing rules.

| Script | Establishes |
|---|---|
| `snapshot.py` | Full Claude corpus snapshot — dedupe, cache tiers, model normalization, main/subagent split, cost |
| `cost.py` | Per-model / per-project / per-day cost breakdown |
| `codex_check.py` | Proves summing `last_token_usage` over-counts (~2×) — why deltas are derived by diffing |
| `codex_check2.py` | Proves cumulative totals are monotonic (155/159) and quantifies the 42.7% duplicate rate |

Run against the live corpus; they read `~/.claude` and `~/.codex` read-only.

`snapshot.py --json` emits the Claude snapshot as stable structured data for
field-by-field differential checks against `llm-usage ingest --full --json`
(`.tools.claude` plus `.ingest.by_tool.claude`).

Run the end-to-end comparison against a built binary with:

```sh
python3 testdata/reference/compare.py --binary ./llm-usage
```
