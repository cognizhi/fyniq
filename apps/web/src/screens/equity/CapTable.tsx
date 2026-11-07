import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { FileDown, Plus, ShieldCheck, TriangleAlert } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useMode } from "../../mode";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { save } from "../reports/AccountantExports";
import { EquityTabs } from "./Holders";
import { drafters, kindWords, listTransactions } from "./transactionWords";

type CapTableView = components["schemas"]["CapTableView"];
type CapTableAt = components["schemas"]["CapTableAt"];
type CapLine = components["schemas"]["CapTableLine"];
type CapHolder = components["schemas"]["CapTableHolder"];
type MyHolding = components["schemas"]["MyHolding"];

const forbidden = "API-AUTHZ-01";

async function capTable(at: string, compare: string): Promise<CapTableView> {
  const { data, error } = await api.GET("/v1/cap-table", { params: { query: { at: at || undefined, compare: compare || undefined } } });
  if (error) throw error;
  return data;
}

async function myHolding(at: string): Promise<MyHolding> {
  const { data, error } = await api.GET("/v1/me/holding", { params: { query: { at: at || undefined } } });
  if (error) throw error;
  return data;
}

// shares is a whole number of shares grouped for reading: "1,252,083".
export function shares(s: string): string {
  const [head, tail] = s.split(".");
  const grouped = (head ?? "").replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  return tail ? `${grouped}.${tail}` : grouped;
}

function pct(p?: string): string {
  return p ? `${p}%` : "–";
}

// added is the exact sum of whole numbers of shares, for the holders folded into Other.
function added(xs: string[]): string {
  return xs.reduce((n, x) => n + BigInt(x), 0n).toString();
}

// Ownership is the part-to-whole of the shares issued (FR-EQ-20; MASTER.md §10): one stacked bar, each holder's colour
// in fixed order by their place, at most eight with the rest as Other, a legend naming each with the percentage the
// server computed, and a table view of the same figures.
function Ownership({ t, level }: { t: CapTableAt; level: 2 | 3 }) {
  const id = useId();
  const [view, setView] = useState<"chart" | "table">("chart");
  const Heading = level === 2 ? "h2" : "h3";
  // Ownership is of the shares issued: a holder with options and no shares shows fully diluted only (FR-EQ-19).
  const owners = t.holders.filter((h) => h.issued !== "0");
  const shown = owners.length > 8 ? owners.slice(0, 7) : owners;
  const rest = owners.length > 8 ? owners.slice(7) : [];
  // Each part's width is the percentage the server computed, drawn only (RND-DISPLAY: every figure a person reads is its
  // string), with a gap of 3 thousandths between parts; the holders after the seventh share Other's width.
  let x = 0;
  const parts = [
    ...shown.map((h, i) => ({ key: h.holder_id, colour: `fy-c${i + 1}`, share: Number(h.ownership ?? "0") })),
    ...(rest.length > 0 ? [{ key: "other", colour: "fy-cother", share: 100 - shown.reduce((n, h) => n + Number(h.ownership ?? "0"), 0) }] : []),
  ].map((p, i, all) => {
    const out = { key: p.key, colour: p.colour, x, width: Math.max(0, p.share * 10 - (i < all.length - 1 ? 3 : 0)) };
    x += p.share * 10;
    return out;
  });
  const summary =
    owners.length === 0
      ? "No shares are issued on this day."
      : `${shares(t.issued)} shares issued. ` + owners.map((h) => `${h.holder_name} ${pct(h.ownership)}`).join(", ") + ".";
  return (
    <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head">
        <Heading className="fy-h3" id={`${id}-title`} title={helpText("cap-table.ownership").tooltip}>
          Ownership
        </Heading>
        <div className="fy-segmented" role="radiogroup" aria-label="Show ownership as">
          {(["chart", "table"] as const).map((k) => (
            <button key={k} type="button" role="radio" aria-checked={view === k} onClick={() => setView(k)}>
              {k === "chart" ? "Chart" : "Table"}
            </button>
          ))}
        </div>
      </div>
      {view === "chart" && owners.length > 0 && (
        <div className="fy-stack fy-stack--2">
          <p className="fy-small fy-muted">Issued · {shares(t.issued)} shares</p>
          <svg className="fy-chart" viewBox="0 0 1000 10" preserveAspectRatio="none" style={{ height: 28, borderRadius: 4 }} role="img" aria-label={summary}>
            {parts.map((x) => (
              <rect key={x.key} className={`fy-bar ${x.colour}`} x={x.x} y={0} width={x.width} height={10} />
            ))}
          </svg>
          <div className="fy-legend">
            {shown.map((h, i) => (
              <span key={h.holder_id}>
                <i className={`fy-legend__key fy-swatch-${i + 1}`} aria-hidden="true" />
                {h.holder_name} <span className="fy-num fy-strong">{pct(h.ownership)}</span>
              </span>
            ))}
            {rest.length > 0 && (
              <span>
                <i className="fy-legend__key fy-swatch-other" aria-hidden="true" />
                Other, {rest.length} holders <span className="fy-num fy-strong">{shares(added(rest.map((h) => h.issued)))} shares</span>
              </span>
            )}
          </div>
        </div>
      )}
      {view === "chart" && owners.length === 0 && <p className="fy-small fy-muted">{summary}</p>}
      {view === "table" && (
        <table className="fy-table" aria-label="Ownership">
          <thead>
            <tr>
              <th scope="col">Holder</th>
              <th scope="col" className="fy-num">
                Shares
              </th>
              <th scope="col" className="fy-num">
                Owns
              </th>
            </tr>
          </thead>
          <tbody>
            {owners.map((h) => (
              <tr key={h.holder_id}>
                <td>{h.holder_name}</td>
                <td className="fy-num">{shares(h.issued)}</td>
                <td className="fy-num">{pct(h.ownership)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {t.fully_diluted === t.issued ? (
        <p className="fy-small fy-muted">Fully diluted is the same as issued: no options are granted and no option pool is recorded.</p>
      ) : (
        <p className="fy-small fy-secondary">
          Fully diluted · {shares(t.fully_diluted)} shares, with {shares(t.options)} options granted and {shares(t.unallocated_pool)} left in the
          option pool
        </p>
      )}
    </section>
  );
}

// DrillDown lists the movements behind a holding: each transaction, whose page has its evidence, or share issue.
function DrillDown({ line, close }: { line: CapLine; close: () => void }) {
  return (
    <Dialog open onOpenChange={(o) => !o && close()} title={`${line.holder_name}, ${line.class_name}`}
      description={`${shares(line.issued)} shares: the movements they come from, oldest first.`}>
      <table className="fy-table" aria-label="Movements">
        <thead>
          <tr>
            <th scope="col">Effective</th>
            <th scope="col">From</th>
            <th scope="col" className="fy-num">
              Shares
            </th>
          </tr>
        </thead>
        <tbody>
          {line.movements.map((m) => (
            <tr key={m.id}>
              <td>{date(m.effective_on)}</td>
              <td>
                {m.transaction_id ? (
                  <Link to="/equity/transactions/$transactionId" params={{ transactionId: m.transaction_id }}>
                    {m.transaction_number || "Equity transaction"}
                  </Link>
                ) : (
                  <Link to="/equity/transactions">{m.kind === "issue" ? "Register of share issues" : "Register"}</Link>
                )}{" "}
                <span className="fy-small fy-secondary">{m.kind === "class_conversion" ? kindWords.class_conversion.toLowerCase() : m.kind}</span>
              </td>
              <td className="fy-num">{m.shares.startsWith("-") ? `−${shares(m.shares.slice(1))}` : shares(m.shares)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </Dialog>
  );
}

// ByClass is Expert's table by holder and class (FR-EQ-20): issued, ownership and voting, fully diluted, a total, and
// each figure opening the movements it comes from.
function ByClass({ t }: { t: CapTableAt }) {
  const id = useId();
  const [open, setOpen] = useState<CapLine | null>(null);
  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-title`} title={helpText("cap-table.by-class").tooltip}>
          By holder and class
        </h2>
        <span className="fy-small fy-secondary">Select a figure to see its transactions and evidence</span>
      </div>
      <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
        <table className="fy-table fy-table--cards" aria-label="By holder and class">
          <thead>
            <tr>
              <th scope="col">Holder</th>
              <th scope="col">Class</th>
              <th scope="col" className="fy-num">
                Issued
              </th>
              <th scope="col" className="fy-num">
                Issued %
              </th>
              <th scope="col" className="fy-num">
                Voting %
              </th>
              <th scope="col" className="fy-num">
                Fully diluted
              </th>
              <th scope="col" className="fy-num">
                FD %
              </th>
            </tr>
          </thead>
          <tbody>
            {t.lines.map((l) => (
              <tr key={`${l.holder_id}-${l.account_code}-${l.class_id ?? ""}`}>
                <td data-card="title">{l.holder_name}</td>
                <td data-card="meta">
                  {l.class_name}
                  {l.votes_assumed && (
                    <span className="fy-chip fy-chip--warning ms-2" title={helpText("cap-table.votes-assumed").tooltip}>
                      <Icon of={TriangleAlert} />
                      Votes assumed
                    </span>
                  )}
                </td>
                <td data-card="amount" className="fy-num">
                  <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm fy-num" onClick={() => setOpen(l)} aria-label={`${shares(l.issued)} shares of ${l.holder_name}: their movements`}>
                    {shares(l.issued)}
                  </button>
                </td>
                <td className="fy-num">{l.ownership ?? "–"}</td>
                <td className="fy-num">{l.voting ?? "–"}</td>
                <td className="fy-num">
                  {shares(l.fully_diluted)}
                  {l.options && <div className="fy-small fy-muted">incl. {shares(l.options)} options</div>}
                </td>
                <td className="fy-num">{l.diluted ?? "–"}</td>
              </tr>
            ))}
            {t.pool_diluted && (
              <tr>
                <td data-card="title">Unallocated option pool</td>
                <td data-card="meta">Options not granted yet</td>
                <td data-card="amount" className="fy-num">
                  –
                </td>
                <td className="fy-num">–</td>
                <td className="fy-num">–</td>
                <td className="fy-num">{shares(t.unallocated_pool)}</td>
                <td className="fy-num">{t.pool_diluted}</td>
              </tr>
            )}
          </tbody>
          {t.lines.length > 0 && (
            <tfoot>
              <tr>
                <th scope="row" colSpan={2}>
                  Total
                </th>
                <td className="fy-num">{shares(t.issued)}</td>
                <td className="fy-num">{t.issued === "0" ? "–" : "100.00"}</td>
                <td className="fy-num">{t.votes === "0" ? "–" : "100.00"}</td>
                <td className="fy-num">{shares(t.fully_diluted)}</td>
                <td className="fy-num">100.00</td>
              </tr>
            </tfoot>
          )}
        </table>
      </div>
      {open && <DrillDown line={open} close={() => setOpen(null)} />}
    </section>
  );
}

// Since compares the cap table with an earlier day: the shares issued, and each holder's percentage then and now.
function Since({ now, then }: { now: CapTableAt; then: CapTableAt }) {
  const before = new Map(then.holders.map((h) => [h.holder_id, h]));
  const ids = [...new Set([...then.holders.map((h) => h.holder_id), ...now.holders.map((h) => h.holder_id)])];
  const named = new Map([...then.holders, ...now.holders].map((h) => [h.holder_id, h.holder_name]));
  const after = new Map(now.holders.map((h) => [h.holder_id, h]));
  return (
    <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-label={`Since ${date(then.at)}`}>
      <h2 className="fy-h3">Since {date(then.at)}</h2>
      <p className="fy-small">
        {now.issued === then.issued ? `Issued shares unchanged at ${shares(now.issued)}.` : `Issued shares ${shares(then.issued)} → ${shares(now.issued)}.`}
      </p>
      <ul className="fy-small fy-stack fy-stack--1">
        {ids.map((id) => (
          <li key={id}>
            {named.get(id)}: {pct(before.get(id)?.ownership ?? "0.00")} → {pct(after.get(id)?.ownership ?? "0.00")}
          </li>
        ))}
      </ul>
    </section>
  );
}

function Mine({ m }: { m: MyHolding }) {
  if (!m.linked || !m.holder) return null;
  const since = m.lines.flatMap((l) => l.movements.map((x) => x.effective_on)).sort()[0];
  return (
    <section className="fy-tile" aria-label="My holding">
      <span className="fy-tile__label" title={helpText("cap-table.mine").tooltip}>
        My holding
      </span>
      <span className="fy-tile__value fy-num">{pct(m.holder.ownership)}</span>
      <span className="fy-tile__meta">
        {m.lines.map((l) => `${shares(l.issued)} ${l.class_name.toLowerCase()}`).join(", ")}
        {since ? ` since ${date(since)}` : ""}
      </span>
    </section>
  );
}

// CapTable is Equity › Cap table, "Who owns the company" (docs/design/screens CapTable; FR-EQ-20, FR-EQ-27): at any day,
// from the register and never edited. Basic shows the ownership, my holding, the latest round and the shareholders;
// Expert adds the comparison with another day and the table by holder and class with each figure's movements. The Board
// viewer, who reads only their own holding, sees that alone.
export function CapTable() {
  const { session } = useSignedIn();
  const expert = useMode(session.roles) === "expert";
  const mayDraft = (session.roles ?? []).some((r) => drafters.includes(r));
  const [at, setAt] = useState("");
  const [compare, setCompare] = useState("");
  const [busy, setBusy] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const q = useQuery({ queryKey: ["cap-table", at, expert ? compare : ""], queryFn: () => capTable(at, expert ? compare : ""), retry: false });
  const mine = useQuery({ queryKey: ["my-holding", at], queryFn: () => myHolding(at), retry: false });
  const whole = !(q.isError && problemOf(q.error).code === forbidden);
  const txs = useQuery({ queryKey: ["equity-transactions"], queryFn: listTransactions, enabled: whole && q.isSuccess });
  const t = q.data;
  const waiting = (txs.data ?? []).filter((x) => x.status === "approved" && x.details.kind !== "reversal");
  const latest = (txs.data ?? [])
    .filter((x) => x.status === "effective" && x.details.kind === "issue_cash" && x.effective_on)
    .sort((a, b) => (b.effective_on ?? "").localeCompare(a.effective_on ?? ""))[0];
  const exportH = helpText("cap-table.export");
  const download = async (format: "xlsx" | "pdf") => {
    setBusy(format);
    setProblem(null);
    const r = await api.GET("/v1/cap-table/export", { params: { query: { at: at || undefined, format } } });
    setBusy("");
    if (r.error) return setProblem(problemOf(r.error));
    save(r.data.file_name, r.data.media_type, r.data.content_base64);
  };
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Equity</span>
          <h1 className="fy-h1" title={helpText("equity.cap-table").tooltip}>
            Who owns the company
          </h1>
          <p className="fy-secondary">Computed from the equity register, never edited by hand. As at {date(t?.at ?? mine.data?.at)}.</p>
        </div>
        {whole && (
          <div className="fy-pagehead__actions">
            <Button variant="secondary" disabled={busy !== "" || !t} onClick={() => void download("xlsx")} title={exportH.tooltip}>
              <Icon of={FileDown} />
              Excel
            </Button>
            <Button variant="secondary" disabled={busy !== "" || !t} onClick={() => void download("pdf")} title={exportH.tooltip}>
              <Icon of={FileDown} />
              PDF
            </Button>
            {mayDraft && (
              <Link className="fy-btn fy-btn--primary" to="/equity/transactions/new">
                <Icon of={Plus} />
                New transaction
              </Link>
            )}
          </div>
        )}
      </div>
      {whole && <EquityTabs current="cap-table" />}
      <div className="fy-row gap-3" style={{ flexWrap: "wrap" }}>
        <div className="fy-field">
          <label className="fy-label" htmlFor="cap-at">
            As at
          </label>
          <input className="fy-input" id="cap-at" type="date" value={at} onChange={(e) => setAt(e.target.value)} />
        </div>
        {expert && whole && (
          <div className="fy-field">
            <label className="fy-label" htmlFor="cap-compare">
              Compare with
            </label>
            <input className="fy-input" id="cap-compare" type="date" value={compare} onChange={(e) => setCompare(e.target.value)} />
          </div>
        )}
      </div>
      {problem && <Refusal problem={problem} title="The cap table was not exported" />}
      {q.isError && whole && <Refusal problem={problemOf(q.error)} title="The cap table could not be shown" />}
      {!whole && mine.data && !mine.data.linked && (
        <p className="fy-small fy-muted">You do not sign in as a shareholder, so there is no holding of yours to show.</p>
      )}
      <div className="fy-grid fy-grid--tiles">
        {mine.data && <Mine m={mine.data} />}
        {latest && (
          <section className="fy-tile" aria-label="Latest round">
            <span className="fy-tile__label">Latest round</span>
            <span className="fy-tile__value fy-num">{latest.amount ? `${formatAmount(latest.amount.amount)} ${latest.amount.currency}` : "–"}</span>
            <span className="fy-tile__meta">
              Shares for cash from {latest.parties.map((p) => p.name).join(", ")} · {date(latest.effective_on)}
            </span>
          </section>
        )}
      </div>
      {t && (
        <>
          {t.issued === "0" && (
            <p className="fy-small fy-muted">No shares are on the register on this day. Record the shares issued before the books start, or draft a transaction.</p>
          )}
          <Ownership t={t} level={2} />
          {expert && t.compared && <Since now={t} then={t.compared} />}
          {waiting.length > 0 && (
            <div className="fy-banner fy-banner--info" role="note">
              <strong>
                {waiting.length === 1 ? "1 transaction waits" : `${waiting.length} transactions wait`} for ACRA:{" "}
                {waiting.map((x, i) => (
                  <span key={x.id}>
                    {i > 0 && ", "}
                    <Link to="/equity/transactions/$transactionId" params={{ transactionId: x.id }}>
                      {x.number}
                    </Link>
                  </span>
                ))}
              </strong>{" "}
              The shares count here from the day ACRA registers them, not before.
            </div>
          )}
          {expert ? (
            <ByClass t={t} />
          ) : (
            <section className="fy-card" aria-label="Shareholders">
              <table className="fy-table fy-table--cards" aria-label="Shareholders">
                <thead>
                  <tr>
                    <th scope="col">Shareholder</th>
                    <th scope="col">Type of share</th>
                    <th scope="col" className="fy-num">
                      Shares
                    </th>
                    <th scope="col" className="fy-num">
                      Owns
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {t.holders.map((h: CapHolder) => (
                    <tr key={h.holder_id}>
                      <td data-card="title">
                        {h.holder_name}
                        {h.mine ? " (you)" : ""}
                      </td>
                      <td data-card="meta">
                        {t.lines
                          .filter((l) => l.holder_id === h.holder_id)
                          .map((l) => `${l.class_name} · ${shares(l.issued)} shares`)
                          .join("; ")}
                      </td>
                      <td data-card="amount" className="fy-num">
                        {shares(h.issued)}
                      </td>
                      <td className="fy-num">{pct(h.ownership)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </section>
          )}
          <section className="fy-card fy-card--pad fy-row gap-3" aria-label="Restricted information">
            <Icon of={ShieldCheck} />
            <p className="fy-small">
              <strong>Restricted information.</strong> Only people with equity access see this page; the assistant cannot read equity data.
            </p>
          </section>
        </>
      )}
    </div>
  );
}
