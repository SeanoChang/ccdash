"""Price the real Claude transcripts using authoritative per-MTok rates.

Rates from the claude-api skill (cached 2026-06-24), USD per million tokens.
Cache reads ~0.1x input; cache writes 1.25x (5m TTL) / 2.0x (1h TTL).
"""
import json, os, glob, collections, time

PRICE = {  # (input, output) $ per MTok
    "claude-fable-5":   (10.0, 50.0),
    "claude-mythos-5":  (10.0, 50.0),
    "claude-opus-5":    (5.0, 25.0),
    "claude-opus-4-8":  (5.0, 25.0),
    "claude-opus-4-7":  (5.0, 25.0),
    "claude-opus-4-6":  (5.0, 25.0),
    "claude-sonnet-5":  (3.0, 15.0),
    "claude-sonnet-4-6": (3.0, 15.0),
    "claude-haiku-4-5": (1.0, 5.0),
}
CACHE_READ = 0.10
WRITE_5M = 1.25
WRITE_1H = 2.00
M = 1_000_000


def price(model, u):
    rate = PRICE.get(model)
    if rate is None:
        return None  # unknown model -> excluded, reported separately
    inp, out = rate
    cc = u.get("cache_creation") or {}
    w5 = cc.get("ephemeral_5m_input_tokens", 0)
    w1 = cc.get("ephemeral_1h_input_tokens", 0)
    # fall back to the flat field when the split isn't present
    if not (w5 or w1):
        w5 = u.get("cache_creation_input_tokens", 0)
    return (
        u.get("input_tokens", 0) / M * inp
        + u.get("output_tokens", 0) / M * out
        + u.get("cache_read_input_tokens", 0) / M * inp * CACHE_READ
        + w5 / M * inp * WRITE_5M
        + w1 / M * inp * WRITE_1H
    )


t0 = time.time()
root = os.path.expanduser("~/.claude/projects")
seen = set()
by_model = collections.defaultdict(lambda: collections.Counter())
by_model_cost = collections.Counter()
by_project = collections.Counter()
by_day = collections.Counter()
unknown = collections.Counter()
tot = collections.Counter()

for p in glob.iglob(root + "/**/*.jsonl", recursive=True):
    is_sub = "/subagents/" in p
    # project slug = the directory directly under projects/
    rel = os.path.relpath(p, root)
    proj = rel.split(os.sep)[0]
    try:
        fh = open(p, "rb")
    except OSError:
        continue
    with fh:
        for line in fh:
            if b'"usage":{' not in line:
                continue
            try:
                r = json.loads(line)
            except Exception:
                continue
            msg = r.get("message") or {}
            u = msg.get("usage")
            if not u:
                continue
            k = r.get("requestId") or msg.get("id")
            if k in seen:
                continue
            seen.add(k)
            model = msg.get("model") or "?"
            c = price(model, u)
            if c is None:
                unknown[model] += 1
                continue
            day = (r.get("timestamp") or "")[:10]
            by_model_cost[model] += c
            by_model[model]["in"] += u.get("input_tokens", 0)
            by_model[model]["out"] += u.get("output_tokens", 0)
            by_model[model]["cr"] += u.get("cache_read_input_tokens", 0)
            by_model[model]["cw"] += u.get("cache_creation_input_tokens", 0)
            by_model[model]["req"] += 1
            by_project[proj] += c
            by_day[day] += c
            tot["cost"] += c
            tot["sub" if is_sub else "main"] += c

el = time.time() - t0
print(f"priced {len(seen):,} deduped requests in {el:.2f}s")
if unknown:
    print(f"  unpriced models (excluded): {dict(unknown)}")

print(f"\n{'model':<20}{'requests':>9}{'output tok':>13}{'cache read':>13}{'cost':>11}")
print("-" * 66)
for m, c in by_model_cost.most_common():
    d = by_model[m]
    print(f"{m:<20}{d['req']:>9,}{d['out']:>13,}{d['cr']:>13,}{'$' + f'{c:,.2f}':>11}")
print("-" * 66)
print(f"{'TOTAL':<20}{len(seen):>9,}{'':>13}{'':>13}{'$' + f'{tot[chr(99)+chr(111)+chr(115)+chr(116)]:,.2f}':>11}")

print(f"\n  main loop  ${tot['main']:>9,.2f}   ({100*tot['main']/tot['cost']:.1f}%)")
print(f"  subagents  ${tot['sub']:>9,.2f}   ({100*tot['sub']/tot['cost']:.1f}%)")

print("\ntop projects by cost:")
for proj, c in by_project.most_common(6):
    print(f"  ${c:>9,.2f}  {proj[:60]}")

print("\ncost by day:")
mx = max(by_day.values()) if by_day else 1
for day in sorted(by_day):
    bar = "█" * max(1, int(by_day[day] / mx * 34))
    print(f"  {day}  {bar} ${by_day[day]:,.2f}")
