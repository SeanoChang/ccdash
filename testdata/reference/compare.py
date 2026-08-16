"""Run a fresh Go ingest and compare its Claude snapshot to the naive oracle."""

import argparse
import json
import os
import pathlib
import shutil
import subprocess
import tempfile


INTEGER_TOTALS = (
    "requests",
    "tokens",
    "input_tokens",
    "output_tokens",
    "cache_read_tokens",
    "cache_write_tokens",
    "main_tokens",
    "subagent_tokens",
)
COST_TOTALS = (
    "cost_at_api_rates",
    "main_cost_at_api_rates",
    "subagent_cost_at_api_rates",
)
MODEL_INTEGER_FIELDS = (
    "requests",
    "tokens",
    "input_tokens",
    "output_tokens",
    "thinking_tokens",
    "cache_read_tokens",
    "cache_write_5m_tokens",
    "cache_write_1h_tokens",
)


def fail(message):
    raise AssertionError(message)


def compare(go_doc, reference):
    go_snapshot = go_doc["tools"]["claude"]
    go_stats = go_doc["ingest"]["by_tool"]["claude"]
    if go_stats["usage_events"] != reference["usage_events"]:
        fail(f"usage events: Go {go_stats['usage_events']} != reference {reference['usage_events']}")
    if go_stats["duplicate_events"] != reference["duplicates"]:
        fail(f"duplicates: Go {go_stats['duplicate_events']} != reference {reference['duplicates']}")

    for field in INTEGER_TOTALS:
        got = go_snapshot["totals"][field]
        want = reference["totals"][field]
        if got != want:
            fail(f"total {field}: Go {got} != reference {want}")
    for field in COST_TOTALS:
        got = go_snapshot["totals"][field]
        want = reference["totals"][field]
        if abs(got - want) > 1e-7:
            fail(f"total {field}: Go {got} != reference {want}")

    go_models = {row["model"]: row for row in go_snapshot["models"]}
    reference_models = {row["model"]: row for row in reference["models"]}
    if go_models.keys() != reference_models.keys():
        fail(f"model sets differ: Go {sorted(go_models)} != reference {sorted(reference_models)}")
    for name in go_models:
        for field in MODEL_INTEGER_FIELDS:
            got = go_models[name][field]
            want = reference_models[name][field]
            if got != want:
                fail(f"{name} {field}: Go {got} != reference {want}")
        got = go_models[name]["cost_at_api_rates"]
        want = reference_models[name]["cost_at_api_rates"]
        if abs(got - want) > 1e-7:
            fail(f"{name} cost: Go {got} != reference {want}")
    if go_snapshot["unpriced"] != reference["unpriced"]:
        fail(f"unpriced: Go {go_snapshot['unpriced']} != reference {reference['unpriced']}")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True, help="path to the ccdash binary")
    args = parser.parse_args()
    binary = str(pathlib.Path(args.binary).resolve())
    snapshot_script = pathlib.Path(__file__).with_name("snapshot.py")
    source_home = pathlib.Path.home()
    with tempfile.TemporaryDirectory(prefix="ccdash-differential-") as temp:
        snapshot_home = pathlib.Path(temp, "home")
        snapshot_claude = snapshot_home / ".claude"
        snapshot_claude.mkdir(parents=True)
        source_projects = source_home / ".claude" / "projects"
        snapshot_projects = snapshot_claude / "projects"
        try:
            subprocess.run(
                ["cp", "-cR", str(source_projects), str(snapshot_projects)],
                check=True,
                capture_output=True,
            )
        except subprocess.CalledProcessError:
            shutil.copytree(source_projects, snapshot_projects)
        source_cache = source_home / ".claude.json"
        if source_cache.exists():
            shutil.copy2(source_cache, snapshot_home / ".claude.json")
        env = os.environ.copy()
        env["HOME"] = str(snapshot_home)
        env["XDG_CONFIG_HOME"] = os.path.join(temp, "config")
        env["XDG_DATA_HOME"] = os.path.join(temp, "data")
        go_run = subprocess.run(
            [binary, "--db", os.path.join(temp, "usage.db"), "ingest", "--full", "--json"],
            check=True,
            capture_output=True,
            text=True,
            env=env,
        )
        reference_run = subprocess.run(
            ["python3", str(snapshot_script), "--json"],
            check=True,
            capture_output=True,
            text=True,
            env=env,
        )
    go_doc = json.loads(go_run.stdout)
    reference = json.loads(reference_run.stdout)
    compare(go_doc, reference)
    totals = reference["totals"]
    print(
        f"PASS: {totals['requests']:,} Claude requests, "
        f"{totals['tokens'] / 1_000_000:,.1f}M tokens, "
        f"${totals['cost_at_api_rates']:,.2f} at API rates"
    )


if __name__ == "__main__":
    main()
