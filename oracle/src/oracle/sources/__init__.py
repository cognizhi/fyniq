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
from oracle.sources import assets, bank, claims, equity, founders, merge, purchases, sales

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
    "fixed_asset": assets.fixed_asset,
    "asset_disposal": assets.asset_disposal,
    "pre_incorporation_cost": founders.pre_incorporation_cost,
    "director_fee": founders.director_fee,
    "director_waiver": founders.director_waiver,
    "director_repayment": founders.director_repayment,
    "director_conversion": founders.director_conversion,
    "account_merge": merge.account_merge,
    "share_issue_cash": equity.share_issue_cash,
    "share_transfer": equity.share_transfer,
    "instrument": equity.instrument,
    "safe_conversion": equity.safe_conversion,
    "director_safe": equity.director_safe,
}
