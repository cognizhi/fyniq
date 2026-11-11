"""The oracle's command line.

    python -m oracle run <scenario.json> [--explain]
        Post a fyniq-scenario@1 scenario and write its fyniq-scenario-result@1 JSON to stdout.
        --explain writes why each document or reversal was rejected to stderr (never to the result).
    python -m oracle generate --seed <n> --count <n> --out <dir>
        Write <count> generated scenarios to <dir>/<name>.json (task 2.4).
    python -m oracle forecast <forecast.json>
        Run every scenario of a fyniq-forecast@1 document and write its fyniq-forecast-result@1 JSON to stdout.
    python -m oracle generate-forecasts --seed <n> --count <n> --out <dir>
        Write <count> generated forecast documents to <dir>/<name>.json (p1-planning task 2.3).
    python -m oracle feasibility <study.json>
        Work a fyniq-feasibility@1 study out and write its fyniq-feasibility-result@1 JSON to stdout.
    python -m oracle generate-feasibilities --seed <n> --count <n> --out <dir>
        Write <count> generated feasibility studies to <dir>/<name>.json (p1-planning task 7.2).
    python -m oracle purge <purge.json>
        Purge a fyniq-purge@1 ledger and write its fyniq-purge-result@1 JSON to stdout (p1-audit-explorer task 8.2).
    python -m oracle generate-purges --seed <n> --count <n> --out <dir>
        Write <count> generated purge documents to <dir>/<name>.json.

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


def _forecast(args: argparse.Namespace) -> int:
    from oracle.planning import ForecastError, forecast
    from oracle.scenario import parse_json

    try:
        doc = parse_json(Path(args.forecast).read_text(encoding="utf-8"))
        result = forecast(doc)
    except (ForecastError, ScenarioError, OSError) as exc:
        print(f"oracle: {args.forecast}: {exc}", file=sys.stderr)
        return 2
    sys.stdout.write(_dump(result))
    return 0


def _generate_forecasts(args: argparse.Namespace) -> int:
    from oracle.planning import forecast
    from oracle.strategies_planning import generate  # Hypothesis is needed only here

    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    for doc in generate(args.seed, args.count):
        forecast(doc)  # every generated forecast is valid input
        (out / f"{doc['name']}.json").write_text(_dump(doc), encoding="utf-8")
    print(f"oracle: wrote {args.count} forecasts to {out}", file=sys.stderr)
    return 0


def _feasibility(args: argparse.Namespace) -> int:
    from oracle.feasibility import feasibility
    from oracle.planning import ForecastError
    from oracle.scenario import parse_json

    try:
        doc = parse_json(Path(args.study).read_text(encoding="utf-8"))
        result = feasibility(doc)
    except (ForecastError, ScenarioError, OSError) as exc:
        print(f"oracle: {args.study}: {exc}", file=sys.stderr)
        return 2
    sys.stdout.write(_dump(result))
    return 0


def _generate_feasibilities(args: argparse.Namespace) -> int:
    from oracle.feasibility import feasibility
    from oracle.strategies_planning import generate_feasibilities  # Hypothesis is needed only here

    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    for doc in generate_feasibilities(args.seed, args.count):
        feasibility(doc)  # every generated study is valid input
        (out / f"{doc['name']}.json").write_text(_dump(doc), encoding="utf-8")
    print(f"oracle: wrote {args.count} feasibility studies to {out}", file=sys.stderr)
    return 0


def _purge(args: argparse.Namespace) -> int:
    from oracle.purge import purge
    from oracle.scenario import parse_json

    try:
        doc = parse_json(Path(args.purge).read_text(encoding="utf-8"))
        result = purge(doc)
    except (ScenarioError, OSError) as exc:
        print(f"oracle: {args.purge}: {exc}", file=sys.stderr)
        return 2
    sys.stdout.write(_dump(result))
    return 0


def _generate_purges(args: argparse.Namespace) -> int:
    from oracle.purge import purge
    from oracle.strategies_purge import generate_purges  # Hypothesis is needed only here

    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    for doc in generate_purges(args.seed, args.count):
        purge(doc)  # every generated ledger is valid input
        (out / f"{doc['name']}.json").write_text(_dump(doc), encoding="utf-8")
    print(f"oracle: wrote {args.count} purge documents to {out}", file=sys.stderr)
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

    fc = commands.add_parser("forecast", help="run a forecast document and write its result JSON to stdout")
    fc.add_argument("forecast", help="a fyniq-forecast@1 JSON file")
    fc.set_defaults(handler=_forecast)

    genf = commands.add_parser("generate-forecasts", help="write generated forecast documents to a directory")
    genf.add_argument("--seed", type=int, required=True)
    genf.add_argument("--count", type=_positive, required=True)
    genf.add_argument("--out", required=True, help="the directory to write <name>.json files to")
    genf.set_defaults(handler=_generate_forecasts)

    fe = commands.add_parser("feasibility", help="work a feasibility study out and write its result JSON to stdout")
    fe.add_argument("study", help="a fyniq-feasibility@1 JSON file")
    fe.set_defaults(handler=_feasibility)

    genfe = commands.add_parser("generate-feasibilities", help="write generated feasibility studies to a directory")
    genfe.add_argument("--seed", type=int, required=True)
    genfe.add_argument("--count", type=_positive, required=True)
    genfe.add_argument("--out", required=True, help="the directory to write <name>.json files to")
    genfe.set_defaults(handler=_generate_feasibilities)

    pu = commands.add_parser("purge", help="purge a ledger and write its result JSON to stdout")
    pu.add_argument("purge", help="a fyniq-purge@1 JSON file")
    pu.set_defaults(handler=_purge)

    genpu = commands.add_parser("generate-purges", help="write generated purge documents to a directory")
    genpu.add_argument("--seed", type=int, required=True)
    genpu.add_argument("--count", type=_positive, required=True)
    genpu.add_argument("--out", required=True, help="the directory to write <name>.json files to")
    genpu.set_defaults(handler=_generate_purges)

    args = parser.parse_args(argv)
    return args.handler(args)
