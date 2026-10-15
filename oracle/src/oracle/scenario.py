"""The scenario format ``fyniq-scenario@1`` and its result ``fyniq-scenario-result@1`` (task 2.1).

Both the engine and the oracle read a scenario and write a result; the JSON Schemas in
``test/differential/`` are the contract, and ``test/differential/README.md`` states the rules the
schemas cannot express. This module loads a scenario, checks it against the schema and those rules,
and checks a result before it is written.

A problem with the scenario's *structure* (the schema, duplicate keys or IDs, an impossible date, a
base currency missing from ``currencies``) is a :class:`ScenarioError`: the scenario is not valid
input and nothing is computed. A problem with a *document's content* (an amount, a rate, a currency
code, an account, the balance) is not an error here: the ledger rejects that document.
"""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from datetime import date
from decimal import Decimal
from functools import cache
from pathlib import Path
from typing import Any

from jsonschema import Draft202012Validator
from jsonschema.exceptions import best_match

SCENARIO_SCHEMA = "fyniq-scenario@1"
RESULT_SCHEMA = "fyniq-scenario-result@1"


def _schema_dir() -> Path:
    # oracle/src/oracle/scenario.py -> the repository root is three levels above the package.
    return Path(__file__).resolve().parents[3] / "test" / "differential"


# The schema's patterns, checked again with fullmatch: Python's re.search lets "$" match before a
# final newline, so "JNL-0001\n" passes the schema here but not in Go (RE2) or ECMA-262.
_DATE = re.compile(r"[0-9]{4}-[0-9]{2}-[0-9]{2}")
_NAME = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,99}")
_CURRENCY = re.compile(r"[A-Z]{3}")
_ACCOUNT = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,31}")
_DOCUMENT_ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,63}")


class ScenarioError(ValueError):
    """The input is not a valid ``fyniq-scenario@1`` document."""


@dataclass(frozen=True)
class Account:
    code: str
    type: str
    group: bool
    active: bool


@dataclass(frozen=True)
class Line:
    """A journal line as written. Its amount and rate stay strings: the ledger checks them."""

    account: str
    currency: str
    amount: str
    fx_rate: str | None
    fx_rate_source: str | None


@dataclass(frozen=True)
class Document:
    id: str
    version: int
    date: date
    memo: str
    lines: tuple[Line, ...]


@dataclass(frozen=True)
class Reversal:
    document: str
    date: date


@dataclass(frozen=True)
class Scenario:
    name: str
    base_currency: str
    currencies: dict[str, int]
    accounts: dict[str, Account]
    documents: tuple[Document, ...]
    reversals: tuple[Reversal, ...]


@cache
def _validator(file_name: str) -> Draft202012Validator:
    path = _schema_dir() / file_name
    try:
        schema = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:  # pragma: no cover - a broken checkout
        raise RuntimeError(f"the schema {path} is missing; run the oracle from a full checkout") from exc
    Draft202012Validator.check_schema(schema)
    return Draft202012Validator(schema)


def _schema_error(file_name: str, instance: Any) -> str | None:
    error = best_match(_validator(file_name).iter_errors(instance))
    if error is None:
        return None
    where = "/".join(str(part) for part in error.absolute_path) or "(root)"
    return f"{file_name}: {where}: {error.message}"


def _no_duplicate_keys(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ScenarioError(f"duplicate key {key!r} in a JSON object")
        result[key] = value
    return result


def _no_constants(name: str) -> Any:
    raise ScenarioError(f"{name} is not JSON")


def parse_json(text: str) -> Any:
    """Strict JSON: no duplicate keys, no NaN or Infinity, and never binary floating point (ENG-10).

    A JSON number with a fraction becomes a Decimal, which then fails the schema wherever an integer
    or a string is required.
    """
    try:
        return json.loads(
            text,
            object_pairs_hook=_no_duplicate_keys,
            parse_float=Decimal,
            parse_constant=_no_constants,
        )
    except json.JSONDecodeError as exc:
        raise ScenarioError(f"not JSON: {exc}") from exc


def _date(text: str, where: str) -> date:
    # The shape again (see _DATE); fromisoformat then refuses an impossible day.
    if not _DATE.fullmatch(text):
        raise ScenarioError(f"{where}: {text!r} is not a YYYY-MM-DD date")
    try:
        return date.fromisoformat(text)
    except ValueError as exc:
        raise ScenarioError(f"{where}: {text!r} is not a calendar date") from exc


def _shape(pattern: re.Pattern[str], text: str, where: str) -> None:
    if not pattern.fullmatch(text):
        raise ScenarioError(f"{where}: {text!r} does not match the schema's pattern")


def scenario_from_json(data: Any) -> Scenario:
    """Check ``data`` against ``scenario.schema.json`` and the rules beside it; build a Scenario."""
    problem = _schema_error("scenario.schema.json", data)
    if problem is not None:
        raise ScenarioError(problem)
    _shape(_NAME, data["name"], "name")
    _shape(_CURRENCY, data["base_currency"], "base_currency")
    for code in data["currencies"]:
        _shape(_CURRENCY, code, "currencies")
    currencies = dict(data["currencies"])
    base = data["base_currency"]
    if base not in currencies:
        raise ScenarioError(f"base_currency: {base} is not in currencies")

    accounts: dict[str, Account] = {}
    for i, raw in enumerate(data["accounts"]):
        _shape(_ACCOUNT, raw["code"], f"accounts/{i}/code")
        if raw["code"] in accounts:
            raise ScenarioError(f"accounts/{i}: the code {raw['code']!r} appears twice")
        accounts[raw["code"]] = Account(raw["code"], raw["type"], raw["group"], raw["active"])

    documents: list[Document] = []
    seen: set[str] = set()
    for i, raw in enumerate(data["documents"]):
        _shape(_DOCUMENT_ID, raw["id"], f"documents/{i}/id")
        if raw["id"] in seen:
            raise ScenarioError(f"documents/{i}: the id {raw['id']!r} appears twice")
        seen.add(raw["id"])
        lines = tuple(
            Line(
                account=ln["account"],
                currency=ln["currency"],
                amount=ln["amount"],
                fx_rate=ln.get("fx_rate"),
                fx_rate_source=ln.get("fx_rate_source"),
            )
            for ln in raw["lines"]
        )
        documents.append(
            Document(raw["id"], raw["version"], _date(raw["date"], f"documents/{i}/date"), raw["memo"], lines)
        )

    reversals = tuple(
        Reversal(raw["document"], _date(raw["date"], f"reversals/{i}/date"))
        for i, raw in enumerate(data["reversals"])
    )
    return Scenario(data["name"], base, currencies, accounts, tuple(documents), reversals)


def load_scenario(path: str | Path) -> Scenario:
    """Read, parse and check a scenario file."""
    try:
        text = Path(path).read_text(encoding="utf-8")
    except OSError as exc:
        raise ScenarioError(f"cannot read {path}: {exc.strerror}") from exc
    return scenario_from_json(parse_json(text))


def check_result(result: dict[str, Any]) -> None:
    """Check a result against ``result.schema.json`` before anyone reads it.

    A failure here is a defect in the oracle, not in the scenario.
    """
    problem = _schema_error("result.schema.json", result)
    if problem is not None:
        raise RuntimeError(f"the oracle wrote an invalid result: {problem}")
