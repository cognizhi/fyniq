import { useQuery } from "@tanstack/react-query";
import { Link, useSearch } from "@tanstack/react-router";
import { Download, Plus, ScrollText, X } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { csvCell } from "../../format/csv";
import { date, timeZone } from "../../format/date";
import { sign } from "../../format/decimal";
import { helpText } from "../../i18n";
import { Icon, problemOf } from "../../ui";
import { listAccounts, postable } from "./data";

type Report = components["schemas"]["GeneralLedgerReport"];
type AccountLedger = components["schemas"]["AccountLedger"];
type Line = components["schemas"]["LedgerLine"];

function today(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
}


// csvOf is the report as CSV: one row per line, with each account's opening and closing balance.
export function csvOf(r: Report): string {
  const rows = [["Account", "Name", "Date", "Number", "Source", "Memo", "Description", "Debit", "Credit", "Balance"].join(",")];
  for (const a of r.accounts) {
    const head = [csvCell(a.code), csvCell(a.name)];
    rows.push([...head, csvCell(r.from), "", "", csvCell("Opening balance"), "", "", "", csvCell(a.opening.amount, true)].join(","));
    for (const l of a.lines) {
      const s = sign(l.base.amount);
      rows.push(
        [
          ...head,
          csvCell(l.posting_date),
          csvCell(l.number ?? ""),
          csvCell(l.source_type),
          csvCell(l.memo ?? ""),
          csvCell(l.description ?? ""),
          s > 0 ? csvCell(l.base.amount, true) : "",
          s < 0 ? csvCell(l.base.amount.slice(1), true) : "",
          csvCell(l.balance.amount, true),
        ].join(","),
      );
    }
    rows.push([...head, csvCell(r.to), "", "", csvCell("Closing balance"), "", csvCell(a.debits.amount, true), csvCell(a.credits.amount, true), csvCell(a.closing.amount, true)].join(","));
  }
  return rows.join("\r\n") + "\r\n";
}

function download(name: string, text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "text/csv;charset=utf-8" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

function Source({ l }: { l: Line }) {
  const label = l.number ?? (l.source_type === "opening_balance" ? "Opening" : "Journal");
  if (l.source_type === "manual_journal")
    return (
      <Link className="fy-mono" to="/accounting/journals/$journalId" params={{ journalId: l.source_id }}>
        {label}
      </Link>
    );
  if (l.source_type === "opening_balance")
    return (
      <Link className="fy-mono" to="/accounting/opening-balances">
        {label}
      </Link>
    );
  return <span className="fy-mono">{label}</span>;
}

function Ledger({ a, from, to, currency }: { a: AccountLedger; from: string; to: string; currency: string }) {
  const id = useId();
  return (
    <section className="fy-stack fy-stack--3" aria-labelledby={id}>
      <div className="fy-row fy-row--between fy-row--wrap">
        <h2 className="fy-h3" id={id}>
          <span className="fy-mono fy-muted">{a.code}</span> {a.name} ({currency})
        </h2>
        <span className="fy-small fy-muted">
          {a.lines.length} {a.lines.length === 1 ? "line" : "lines"}
        </span>
      </div>
      <div className="fy-tablewrap">
        <table className="fy-table fy-table--compact fy-table--cards">
          <caption className="fy-sr">
            Ledger lines for account {a.code}, {date(from)} to {date(to)}, {currency}
          </caption>
          <thead>
            <tr>
              <th scope="col">Date · source</th>
              <th scope="col">Description</th>
              <th scope="col" className="fy-num">
                Debit
              </th>
              <th scope="col" className="fy-num">
                Credit
              </th>
              <th scope="col" className="fy-num">
                Balance
              </th>
            </tr>
          </thead>
          <tbody>
            <tr className="fy-group">
              <td data-card="title" colSpan={4}>
                Opening balance, {date(from)}
              </td>
              <td className="fy-num" data-card="status">
                {formatAmount(a.opening.amount)}
              </td>
            </tr>
            {a.lines.map((l) => {
              const s = sign(l.base.amount);
              return (
                <tr key={`${l.entry_id}-${l.ledger_revision}-${l.posting_date}-${l.description ?? ""}-${l.base.amount}`}>
                  <td data-card="meta" className="whitespace-nowrap">
                    {date(l.posting_date)} · <Source l={l} />
                    <span className="fy-only-compact"> · {s > 0 ? "Debit" : "Credit"}</span>
                  </td>
                  <td data-card="title">
                    {l.memo}
                    {l.description && l.description !== l.memo ? <span className="fy-secondary"> · {l.description}</span> : null}
                    {l.amount.currency !== currency && (
                      <span className="fy-small fy-muted fy-mono">
                        {" "}
                        ({formatAmount(l.amount.amount)} {l.amount.currency})
                      </span>
                    )}
                  </td>
                  <td className="fy-num" data-card={s > 0 ? "amount" : "hide"}>
                    {s > 0 ? formatAmount(l.base.amount) : ""}
                  </td>
                  <td className="fy-num" data-card={s < 0 ? "amount" : "hide"}>
                    {s < 0 ? formatAmount(l.base.amount.slice(1)) : ""}
                  </td>
                  <td className="fy-num" data-card="status">
                    <span className="fy-only-compact fy-muted">Balance </span>
                    <span>{formatAmount(l.balance.amount)}</span>
                  </td>
                </tr>
              );
            })}
          </tbody>
          <tfoot>
            <tr>
              <td>Closing balance, {date(to)}</td>
              <td data-card="hide" />
              <td className="fy-num" data-card="hide">
                {formatAmount(a.debits.amount)}
              </td>
              <td className="fy-num" data-card="hide">
                {formatAmount(a.credits.amount)}
              </td>
              <td className="fy-num">{formatAmount(a.closing.amount)}</td>
            </tr>
          </tfoot>
        </table>
      </div>
    </section>
  );
}

// GeneralLedger is Reports › General ledger (docs/design/screens GeneralLedger; FR-GL-11): every posted line of the
// accounts chosen over a range of dates, with the balance before, a running balance and the balance at the end, each
// line opening its source; read at one ledger revision; exported as CSV. Dimensions, saved views and the background
// export of larger ranges arrive with p1-reports.
export function GeneralLedger() {
  const id = useId();
  // A statement's figure opens it with its accounts and run (FR-UX-04).
  const search = useSearch({ strict: false }) as { account_id?: string[] | string; from?: string; to?: string };
  const first = `${today().slice(0, 8)}01`;
  const [from, setFrom] = useState(search.from ?? first);
  const [to, setTo] = useState(search.to ?? today);
  const [chosen, setChosen] = useState<string[]>(() => (search.account_id === undefined ? [] : ([] as string[]).concat(search.account_id).slice(0, 20)));
  const [adding, setAdding] = useState("");
  const accounts = useQuery({ queryKey: ["accounts"], queryFn: listAccounts });
  const report = useQuery({
    queryKey: ["general-ledger", chosen, from, to],
    enabled: chosen.length > 0 && Boolean(from) && Boolean(to),
    queryFn: async () => {
      const r = await api.GET("/v1/reports/general-ledger", { params: { query: { account_id: chosen, from, to } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const choices = postable(accounts.data ?? []).filter((a) => !chosen.includes(a.id));
  const byId = new Map((accounts.data ?? []).map((a) => [a.id, a]));
  const exp = helpText("ledger.export");

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting">Accounting</Link>
          </nav>
          <h1 className="fy-h1">General ledger</h1>
          <p className="fy-secondary">Every posted line of the accounts you choose, with a running balance. Select a source to open it.</p>
        </div>
        {report.data && (
          <div className="fy-pagehead__actions">
            <Button onClick={() => download(`general-ledger-${from}-to-${to}.csv`, csvOf(report.data))} title={exp.tooltip}>
              <Icon of={Download} />
              {exp.label}
            </Button>
          </div>
        )}
      </div>
      <ReviewBanner screen="general-ledger" />
      <div className="fy-card fy-card--pad fy-stack fy-stack--3">
        <div className="fy-field">
          <span className="fy-label" id={`${id}-accounts`}>
            Accounts
          </span>
          <div className="fy-row fy-row--wrap gap-2" role="group" aria-labelledby={`${id}-accounts`}>
            {chosen.map((a) => (
              <span key={a} className="fy-tag">
                <span className="fy-mono">{byId.get(a)?.code}</span> {byId.get(a)?.name}
                <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" aria-label={`Remove ${byId.get(a)?.code ?? "the account"}`} onClick={() => setChosen(chosen.filter((x) => x !== a))}>
                  <Icon of={X} />
                </button>
              </span>
            ))}
            {chosen.length < 20 && (
              <span className="fy-row gap-2">
                <label className="fy-sr" htmlFor={`${id}-add`}>
                  Add an account
                </label>
                <select className="fy-select" id={`${id}-add`} value={adding} onChange={(e) => setAdding(e.target.value)}>
                  <option value="">Choose an account</option>
                  {choices.map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.code} {a.name}
                    </option>
                  ))}
                </select>
                <Button
                  size="sm"
                  disabled={!adding}
                  onClick={() => {
                    setChosen([...chosen, adding]);
                    setAdding("");
                  }}
                >
                  <Icon of={Plus} />
                  Add
                </Button>
              </span>
            )}
          </div>
        </div>
        <div className="fy-form fy-form--2">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-from`}>
              From
            </label>
            <input className="fy-input" id={`${id}-from`} type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-to`}>
              To
            </label>
            <input className="fy-input" id={`${id}-to`} type="date" value={to} onChange={(e) => setTo(e.target.value)} />
          </div>
        </div>
      </div>
      {chosen.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={ScrollText} size={24} />
          <h2 className="fy-h3">Choose an account</h2>
          <p className="fy-secondary">Add one or more accounts to see their lines between the two dates.</p>
        </div>
      )}
      {report.isError && <Refusal problem={problemOf(report.error)} title="The general ledger could not be shown" />}
      {report.isFetching && !report.data && <p className="fy-secondary">Reading the ledger…</p>}
      {report.data && (
        <>
          {report.data.accounts.map((a) => (
            <Ledger key={a.account_id} a={a} from={report.data.from} to={report.data.to} currency={report.data.currency} />
          ))}
          <p className="fy-small fy-muted">Read at ledger revision {report.data.ledger_revision}: the same revision always gives the same report.</p>
        </>
      )}
    </div>
  );
}
