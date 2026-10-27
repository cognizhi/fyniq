"""The P1 sources' rule sets (QA-ENG-01), one module per source, each written from PRD-001.

Each rule set is a function ``(rules, document, books) -> Posting`` that raises
:class:`~oracle.book.Rejected` when the document cannot post. ``books`` holds what earlier documents
left for later ones to read (a bill's open amount, its stored rate), keyed by document ID.
"""

from __future__ import annotations

from collections.abc import Callable
from typing import Any

from oracle.book import Posting
from oracle.rules import Rules
from oracle.scenario import Source
from oracle.sources import bank, claims, purchases, sales

RuleSet = Callable[[Rules, Source, dict[str, Any]], Posting]

RULE_SETS: dict[str, RuleSet] = {
    "bank_transaction": bank.transaction,
    "bank_transfer": bank.transfer,
    "bill": purchases.bill,
    "vendor_credit": purchases.vendor_credit,
    "bill_payment": purchases.bill_payment,
    "invoice": sales.invoice,
    "credit_note": sales.credit_note,
    "customer_receipt": sales.customer_receipt,
    "customer_deposit": sales.customer_deposit,
    "deposit_refund": sales.deposit_refund,
    "write_off": sales.write_off,
    "expense_claim": claims.expense_claim,
    "claim_reimbursement": claims.claim_reimbursement,
    "cash_advance": claims.cash_advance,
}
