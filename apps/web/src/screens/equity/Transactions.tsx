import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { Refusal } from "../../components/Refusal";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";
import { ShareIssueRegister } from "../founders/ShareIssues";
import { EquityTabs, listHolders } from "./Holders";
import { describe, drafters, holdersText, listTransactions, sharesText, StatusChip, type EquityTransaction } from "./transactionWords";

type Filter = "all" | EquityTransaction["status"];

const filters: [Filter, string][] = [
  ["all", "All"],
  ["approved", "Waiting for ACRA"],
  ["effective", "Effective"],
  ["reversed", "Reversed"],
  ["draft", "Draft"],
];

// EquityTransactions is Equity › Transactions (docs/design/screens EquityRegister; FR-EQ-17, FR-EQ-08, FR-EQ-09 Expert):
// every issue for cash, transfer, conversion between classes and reversal, the latest first, with its ACRA flags; the
// shares held and what waits for ACRA; and below, the register of share issues it writes to.
export function EquityTransactions() {
  const { session } = useSignedIn();
  const mayDraft = (session.roles ?? []).some((r) => drafters.includes(r));
  const [filter, setFilter] = useState<Filter>("all");
  const q = useQuery({ queryKey: ["equity-transactions"], queryFn: listTransactions });
  const holders = useQuery({ queryKey: ["holders"], queryFn: listHolders });
  const add = helpText("equity-transaction.draft");
  const all = q.data ?? [];
  const numbers = useMemo(() => new Map(all.filter((t) => t.number).map((t) => [t.id, t.number ?? ""])), [all]);
  const shown = all.filter((t) => filter === "all" || t.status === filter);
  const waiting = all.filter((t) => t.status === "approved");
  const held = (holders.data ?? []).flatMap((h) => h.holdings).reduce((n, x) => n + Number(x.shares), 0);
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Equity</span>
          <h1 className="fy-h1" title={helpText("equity.transactions").tooltip}>
            Equity transactions
          </h1>
          <p className="fy-secondary">The register of share issues, with every transfer and conversion. Append-only: a mistake is reversed, never edited.</p>
        </div>
        {mayDraft && (
          <div className="fy-pagehead__actions">
            <Link className="fy-btn fy-btn--primary" to="/equity/transactions/new" title={add.tooltip}>
              <Icon of={Plus} />
              {add.label}
            </Link>
          </div>
        )}
      </div>
      <EquityTabs current="transactions" />
      {q.isError && <Refusal problem={problemOf(q.error)} title="The equity transactions could not be shown" />}
      {q.data && (
        <>
          <div className="fy-grid fy-grid--tiles">
            <div className="fy-tile">
              <span className="fy-tile__label">Shares held</span>
              <span className="fy-tile__value fy-num">{held.toLocaleString("en-SG")}</span>
              <span className="fy-tile__meta">Every class, effective</span>
            </div>
            <div className="fy-tile">
              <span className="fy-tile__label">Waiting for ACRA</span>
              <span className="fy-tile__value fy-num">{waiting.length}</span>
              <span className="fy-tile__meta">{waiting.map((t) => t.number).join(", ") || "Nothing approved waits"}</span>
            </div>
          </div>
          <div className="fy-chipset" role="group" aria-label="Show">
            {filters.map(([f, label]) => (
              <button key={f} className="fy-filter" type="button" aria-pressed={filter === f} onClick={() => setFilter(f)}>
                {label} {f === "all" ? all.length : all.filter((t) => t.status === f).length}
              </button>
            ))}
          </div>
          {all.length === 0 ? (
            <p className="fy-small fy-muted">No equity transaction yet. Draft one to issue shares for cash, transfer shares or convert them between classes.</p>
          ) : (
            <div className="fy-card">
              <table className="fy-table fy-table--cards" aria-label="Equity transactions">
                <thead>
                  <tr>
                    <th scope="col">Ref</th>
                    <th scope="col">Effective</th>
                    <th scope="col">Type</th>
                    <th scope="col">Holder</th>
                    <th scope="col" className="fy-from-expanded">
                      Class
                    </th>
                    <th scope="col" className="fy-num">
                      Shares
                    </th>
                    <th scope="col" className="fy-num fy-from-expanded">
                      Amount
                    </th>
                    <th scope="col">Status</th>
                  </tr>
                </thead>
                <tbody className="fy-stagger">
                  {shown.map((t) => (
                    <tr key={t.id}>
                      <td data-card="title" style={{ whiteSpace: "nowrap" }}>
                        <Link className="fy-mono" to="/equity/transactions/$transactionId" params={{ transactionId: t.id }}>
                          {t.number || "Draft"}
                        </Link>
                      </td>
                      <td data-card="hide" className={t.effective_on ? undefined : "fy-muted"} style={{ whiteSpace: "nowrap" }}>
                        {t.effective_on ? date(t.effective_on) : t.status === "approved" ? "At ACRA" : "–"}
                      </td>
                      <td data-card="meta">{describe(t, numbers)}</td>
                      <td data-card="meta">{holdersText(t)}</td>
                      <td data-card="hide" className="fy-from-expanded">
                        <span className="fy-mono">{t.class_code || "–"}</span>
                      </td>
                      <td data-card="amount" className="fy-num">
                        {sharesText(t.details.shares)}
                      </td>
                      <td data-card="hide" className="fy-num fy-from-expanded">
                        {t.amount ? formatAmount(t.amount.amount) : "–"}
                      </td>
                      <td data-card="status">
                        <StatusChip t={t} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
      <ShareIssueRegister titled />
    </div>
  );
}
