"""Single consistent snapshot of the Claude corpus — all acceptance figures
computed in one pass so they cannot drift relative to each other.
"""
import json, os, glob, time, collections, sys

PRICE = {
    "claude-fable-5": (10.0, 50.0), "claude-mythos-5": (10.0, 50.0),
    "claude-opus-5": (5.0, 25.0), "claude-opus-4-8": (5.0, 25.0),
    "claude-opus-4-7": (5.0, 25.0), "claude-opus-4-6": (5.0, 25.0),
    "claude-sonnet-5": (3.0, 15.0), "claude-sonnet-4-6": (3.0, 15.0),
    "claude-haiku-4-5": (1.0, 5.0),
}
M = 1_000_000


def norm(model):
    # strip a trailing -YYYYMMDD
    parts = model.rsplit("-", 1)
    if len(parts) == 2 and len(parts[1]) == 8 and parts[1].isdigit():
        return parts[0]
    return model


t0 = time.time()
root = os.path.expanduser("~/.claude/projects")
seen = set()
c = collections.Counter()
by_model = collections.defaultdict(collections.Counter)
unpriced = collections.Counter()
nfiles = nlines = nusage = 0

for p in glob.iglob(root + "/**/*.jsonl", recursive=True):
    nfiles += 1
    is_sub = "/subagents/" in p
    try:
        fh = open(p, "rb")
    except OSError:
        continue
    with fh:
        for line in fh:
            nlines += 1
            if b'"usage":{' not in line:
                continue
            nusage += 1
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

            inp = u.get("input_tokens", 0)
            out = u.get("output_tokens", 0)
            cr = u.get("cache_read_input_tokens", 0)
            cwf = u.get("cache_creation_input_tokens", 0)
            cc = u.get("cache_creation") or {}
            w5 = cc.get("ephemeral_5m_input_tokens", 0)
            w1 = cc.get("ephemeral_1h_input_tokens", 0)
            if not (w5 or w1):
                w5 = cwf
            cache_write = w5 + w1
            think = (u.get("output_tokens_details") or {}).get("thinking_tokens", 0)

            c["tok"] += inp + out + cr + cache_write
            c["in"] += inp
            c["out"] += out
            c["cr"] += cr
            c["cw"] += cache_write
            c["think"] += think
            c["sub_tok" if is_sub else "main_tok"] += inp + out + cr + cache_write

            m = norm(msg.get("model") or "<unknown>")
            model_totals = by_model[m]
            model_totals["requests"] += 1
            model_totals["tokens"] += inp + out + cr + w5 + w1
            model_totals["input_tokens"] += inp
            model_totals["output_tokens"] += out
            model_totals["thinking_tokens"] += think
            model_totals["cache_read_tokens"] += cr
            model_totals["cache_write_5m_tokens"] += w5
            model_totals["cache_write_1h_tokens"] += w1
            rate = PRICE.get(m)
            if rate is None:
                unpriced[m] += 1
                continue
            ri, ro = rate
            cost = (inp / M * ri + out / M * ro + cr / M * ri * 0.10
                    + w5 / M * ri * 1.25 + w1 / M * ri * 2.00)
            c["cost"] += cost
            c["sub_cost" if is_sub else "main_cost"] += cost
            model_totals["cost_at_api_rates"] += cost

el = time.time() - t0
T = c["tok"]
if "--json" in sys.argv:
    models = []
    for name, values in by_model.items():
        row = {"model": name}
        row.update({
            "requests": values["requests"],
            "tokens": values["tokens"],
            "input_tokens": values["input_tokens"],
            "output_tokens": values["output_tokens"],
            "thinking_tokens": values["thinking_tokens"],
            "cache_read_tokens": values["cache_read_tokens"],
            "cache_write_5m_tokens": values["cache_write_5m_tokens"],
            "cache_write_1h_tokens": values["cache_write_1h_tokens"],
            "cost_at_api_rates": values["cost_at_api_rates"],
        })
        models.append(row)
    models.sort(key=lambda row: (-row["cost_at_api_rates"], -row["requests"], row["model"]))
    print(json.dumps({
        "files": nfiles,
        "usage_events": nusage,
        "duplicates": nusage - len(seen),
        "totals": {
            "requests": len(seen),
            "tokens": T,
            "input_tokens": c["in"],
            "output_tokens": c["out"],
            "cache_read_tokens": c["cr"],
            "cache_write_tokens": c["cw"],
            "main_tokens": c["main_tok"],
            "subagent_tokens": c["sub_tok"],
            "cost_at_api_rates": c["cost"],
            "main_cost_at_api_rates": c["main_cost"],
            "subagent_cost_at_api_rates": c["sub_cost"],
        },
        "models": models,
        "unpriced": dict(unpriced),
    }, indent=2, sort_keys=True))
    raise SystemExit(0)

print(f"snapshot at {time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())}  ({el:.2f}s)")
print(f"  files                {nfiles:,}")
print(f"  lines                {nlines:,}")
print(f"  usage-bearing lines  {nusage:,}")
print(f"  unique requests      {len(seen):,}   (dedup {nusage/max(1,len(seen)):.2f}x)")
print(f"  total tokens         {T/M:,.1f}M")
print(f"    input (fresh)      {c['in']/M:,.1f}M")
print(f"    output             {c['out']/M:,.1f}M   (thinking {c['think']/M:,.1f}M)")
print(f"    cache read         {c['cr']/M:,.1f}M")
print(f"    cache write        {c['cw']/M:,.1f}M")
denom = c["in"] + c["cr"] + c["cw"]
print(f"  cache-read share of input tokens: {100*c['cr']/max(1,denom):.1f}%")
print(f"  main / subagent tokens: {100*c['main_tok']/T:.1f}% / {100*c['sub_tok']/T:.1f}%")
print(f"  cost at API rates    ${c['cost']:,.2f}")
print(f"  main / subagent cost : {100*c['main_cost']/c['cost']:.1f}% / {100*c['sub_cost']/c['cost']:.1f}%")
print(f"  unpriced             {dict(unpriced) or 'none'}")
