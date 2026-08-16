"""Is total_token_usage monotonic non-decreasing within a session?

If yes, diffing consecutive totals telescopes to the final total exactly and is
immune to duplicate token_count events. If totals ever drop (context reset,
session fork), the ingest must treat the drop as a new accumulator epoch.
"""
import json, glob, os, collections

files = sorted(glob.glob(os.path.expanduser("~/.codex/sessions/**/*.jsonl"), recursive=True))
mono = nonmono = 0
drops = []
dupe_events = total_events = 0
telescope_ok = telescope_bad = 0

for p in files:
    totals = []
    for line in open(p, "rb"):
        if b'"token_count"' not in line:
            continue
        try:
            r = json.loads(line)
        except Exception:
            continue
        pl = r.get("payload") or {}
        if pl.get("type") != "token_count":
            continue
        info = pl.get("info") or {}
        t = (info.get("total_token_usage") or {}).get("total_tokens")
        if t is not None:
            totals.append(t)
    if len(totals) < 2:
        continue
    total_events += len(totals)
    dupe_events += sum(1 for a, b in zip(totals, totals[1:]) if a == b)

    bad = [(i, totals[i - 1], totals[i]) for i in range(1, len(totals)) if totals[i] < totals[i - 1]]
    if bad:
        nonmono += 1
        if len(drops) < 5:
            drops.append((os.path.basename(p)[:44], len(totals), bad[:3]))
    else:
        mono += 1

    # telescoping check: totals[0] + sum(diffs) == totals[-1]
    diffsum = totals[0] + sum(max(0, totals[i] - totals[i - 1]) for i in range(1, len(totals)))
    if diffsum == totals[-1]:
        telescope_ok += 1
    else:
        telescope_bad += 1

print(f"sessions with >=2 token_count events : {mono + nonmono}")
print(f"  monotonic non-decreasing           : {mono}")
print(f"  has a DROP                         : {nonmono}")
print(f"\ntoken_count events                   : {total_events:,}")
print(f"  consecutive duplicates (no change) : {dupe_events:,}  ({100*dupe_events/max(1,total_events):.1f}%)")
print(f"\nclamped-diff telescoping to final total:")
print(f"  exact   : {telescope_ok}")
print(f"  off     : {telescope_bad}")
for name, n, bad in drops:
    print(f"\n  DROP {name} ({n} events)")
    for i, prev, cur in bad:
        print(f"    event {i}: {prev:,} -> {cur:,}   (delta {cur-prev:,})")
