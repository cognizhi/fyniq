"""The oracle's command line.

    python -m oracle run <scenario.json> [--explain]
        Post a fyniq-scenario@1 scenario and write its fyniq-scenario-result@1 JSON to stdout.
        --explain writes why each document or reversal was rejected to stderr (never to the result).
    python -m oracle generate --seed <n> --count <n> --out <dir>
        Write <count> generated scenarios to <dir>/<name>.json (task 2.4).

Exit status: 0 on success, 2 when the scenario is not valid input (the reason is on stderr).
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from oracle.ledger import Ledger
from oracle.scenario import ScenarioError, load_scenario, scenario_from_json


def _dump(document: object) -> str:
    return json.dumps(document, indent=2, ensure_ascii=False) + "\n"


def _run(args: argparse.Namespace) -> int:
    try:
        scenario = load_scenario(args.scenario)
    except ScenarioError as exc:
        print(f"oracle: {args.scenario}: {exc}", file=sys.stderr)
        return 2
    ledger = Ledger(scenario)
    sys.stdout.write(_dump(ledger.result()))
    if args.explain:
        for key, reason in ledger.reasons:
            print(f"rejected {key}: {reason}", file=sys.stderr)
    return 0


def _generate(args: argparse.Namespace) -> int:
    from oracle.strategies import generate  # Hypothesis is needed only here

    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    for scenario in generate(args.seed, args.count):
        scenario_from_json(scenario)  # every generated scenario is valid input
        (out / f"{scenario['name']}.json").write_text(_dump(scenario), encoding="utf-8")
    print(f"oracle: wrote {args.count} scenarios to {out}", file=sys.stderr)
    return 0


def _positive(text: str) -> int:
    value = int(text)
    if value < 1:
        raise argparse.ArgumentTypeError("must be at least 1")
    return value


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="python -m oracle", description="Fyniq's independent reference ledger")
    commands = parser.add_subparsers(dest="command", required=True)

    run = commands.add_parser("run", help="post a scenario and write its result JSON to stdout")
    run.add_argument("scenario", help="a fyniq-scenario@1 JSON file")
    run.add_argument("--explain", action="store_true", help="write the reason for each rejection to stderr")
    run.set_defaults(handler=_run)

    gen = commands.add_parser("generate", help="write generated scenarios to a directory")
    gen.add_argument("--seed", type=int, required=True)
    gen.add_argument("--count", type=_positive, required=True)
    gen.add_argument("--out", required=True, help="the directory to write <name>.json files to")
    gen.set_defaults(handler=_generate)

    args = parser.parse_args(argv)
    return args.handler(args)
