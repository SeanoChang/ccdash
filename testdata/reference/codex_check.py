"""Does sum(last_token_usage) == final total_token_usage on Codex rollouts?

If yes, per-turn deltas are a safe canonical form and we never diff cumulative
counters. If no, we must diff consecutive total_token_usage instead.
"""
import json, glob, os

files = sorted(glob.glob(os.path.expanduser("~/.codex/sessions/**/*.jsonl"), recursive=True))
ok = mismatch = empty = 0
examples = []

for p in files:
    summed = {}
    final = None
    n = 0
    try:
        fh = open(p, "rb")
    except OSError:
        continue
    with fh:
        for line in fh:
            if b'"token_count"' not in line:
                continue
            try:
                r = json.loads(line)
            except Exception:
                continue
            pl = r.get("payload") or {}
            if pl.get("type") != "token_count":
                continue
            info = pl.get("info")
            if not info:
                continue
            last = info.get("last_token_usage") or {}
            tot = info.get("total_token_usage") or {}
            if last:
                n += 1
                for k, v in last.items():
                    summed[k] = summed.get(k, 0) + v
            if tot:
                final = tot
    if final is None or n == 0:
        empty += 1
        continue
    keys = ["input_tokens", "cached_input_tokens", "output_tokens", "reasoning_output_tokens", "total_tokens"]
    diffs = {k: summed.get(k, 0) - final.get(k, 0) for k in keys}
    if all(v == 0 for v in diffs.values()):
        ok += 1
    else:
        mismatch += 1
        if len(examples) < 4:
            examples.append((os.path.basename(p)[:46], n, diffs, final.get("total_tokens")))

print(f"files with token_count events : {ok + mismatch}")
print(f"  sum(deltas) == total        : {ok}")
print(f"  MISMATCH                    : {mismatch}")
print(f"  no usable events            : {empty}")
for name, n, d, tot in examples:
    nz = {k: v for k, v in d.items() if v}
    print(f"\n  {name}  ({n} events, final total {tot:,})")
    print(f"    delta-sum minus total: {nz}")
