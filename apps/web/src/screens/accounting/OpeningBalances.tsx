import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CalendarRange, CircleCheck, CircleX, Clock, Eye, Lock, PencilLine, Save, Send, X } from "lucide-react";
import { useId, useState } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date, dateTime } from "../../format/date";
import { isZero, minus, sign, sum, typed } from "../../format/decimal";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listAccounts, postable, type Account } from "./data";
import { preparers, who } from "./Journals";

type Opening = components["schemas"]["OpeningBalances"];
type Posting = components["schemas"]["OpeningPosting"];
type Draft = components["schemas"]["OpeningDraft"];

// Who posts the opening balances: P on "Journals", the Accountant, as for journals (PRD-001 rev 0.49).
const posters = ["accountant"];
const types: [Account["type"], string][] = [
  ["asset", "Assets"],
  ["liability", "Liabilities"],
  ["equity", "Equity"],
  ["revenue", "Revenue"],
  ["expense", "Expenses"],
];
const balanceSheet = new Set<Account["type"]>(["asset", "liability", "equity"]);

export interface Cell {
  debit: string;
  credit: string;
}

// cellsOf is the saved balances by account: a positive amount is a debit balance, a negative one a credit balance.
export function cellsOf(o: Opening | undefined): Record<string, Cell> {
  const out: Record<string, Cell> = {};
  for (const l of o?.lines ?? []) {
    const credit = l.amount.amount.startsWith("-");
    out[l.account_id] = { debit: credit ? "" : l.amount.amount, credit: credit ? l.amount.amount.slice(1) : "" };
  }
  return out;
}

// draftOf turns what was typed into the draft to send, leaving out empty accounts, or says what to fix by account.
export function draftOf(cells: Record<string, Cell>, base: string, memo: string): { draft?: Draft; errors: Record<string, string> } {
  const errors: Record<string, string> = {};
  const lines: Draft["lines"] = [];
  for (const [account, c] of Object.entries(cells)) {
    const d = typed(c.debit);
    const cr = typed(c.credit);
    if (d === null || cr === null) {
      errors[account] = "Enter an amount, such as 1,200.00.";
      continue;
    }
    const hasD = d !== "" && !isZero(d);
    const hasC = cr !== "" && !isZero(cr);
    if (sign(d || "0") < 0 || sign(cr || "0") < 0) errors[account] = "Enter the balance without a minus sign: a credit balance goes under Credit.";
    else if (hasD && hasC) errors[account] = "Enter a debit or a credit balance, not both.";
    else if (hasD || hasC) lines.push({ account_id: account, amount: { amount: hasD ? d : `-${cr}`, currency: base } });
  }
  if (Object.keys(errors).length > 0) return { errors };
  return { draft: { memo: memo.trim() || undefined, lines }, errors };
}

function totals(cells: Record<string, Cell>) {
  const debits = sum(Object.values(cells).map((c) => typed(c.debit) || "0").filter((v) => sign(v) > 0));
  const credits = sum(Object.values(cells).map((c) => typed(c.credit) || "0").filter((v) => sign(v) > 0));
  return { debits, credits, difference: minus(debits, credits) };
}

// OpeningBalances is Accounting › Opening balances (docs/design/screens OpeningBalances; FR-GL-06): every account's
// balance at the books start, as a debit or a credit, adding up to zero. An Accountant or a Tax agent saves them as a
// draft; an Accountant checks and posts them, once. Posted, an Accountant asks to change them, and another person who
// approves journals approves it (FM-1, rev 0.66). Open invoices and bills arrive with sales and purchases.
export function OpeningBalances() {
  const { session, organisation } = useSignedIn();
  const base = organisation?.base_currency ?? "SGD";
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const roles = session.roles ?? [];
  const mayPrepare = roles.some((r) => preparers.includes(r));
  const mayPost = roles.some((r) => posters.includes(r));
  const accounts = useQuery({ queryKey: ["accounts"], queryFn: listAccounts });
  const profile = useQuery({
    queryKey: ["profile", "opening"],
    queryFn: async () => {
      const r = await api.GET("/v1/organisation/profile");
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const current = useQuery({
    queryKey: ["opening-balances"],
    queryFn: async () => {
      const r = await api.GET("/v1/opening-balances");
      if (r.error) throw r.error;
      return { opening: r.data, etag: etagOf(r.response) };
    },
  });
  const o = current.data?.opening;
  const posted = o && (o.status === "posted" || o.status === "reversed");
  const editable = mayPrepare && (o?.status === "none" || o?.status === "draft");
  // Changing posted balances: an Accountant (P on Journals) asks, another person approves (FM-1).
  const [changing, setChanging] = useState(false);
  const [reason, setReason] = useState("");
  const [reasonMissing, setReasonMissing] = useState(false);
  const asking = Boolean(posted && changing && mayPost && !o?.edit_asked);
  const editing = editable || asking;
  const [cells, setCells] = useState<Record<string, Cell> | null>(null);
  const [memo, setMemo] = useState<string | null>(null);
  const [all, setAll] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [failed, setFailed] = useState("");
  const [preview, setPreview] = useState<Posting | null>(null);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState("");
  const c = cells ?? cellsOf(o);
  const m = memo ?? o?.memo ?? "";
  const dirty = cells !== null || memo !== null;
  const t = totals(c);
  const chart = postable(accounts.data ?? []);
  const shown = chart.filter((a) => all || balanceSheet.has(a.type) || c[a.id]);
  const byId = new Map((accounts.data ?? []).map((a) => [a.id, a]));
  const save = helpText("opening.save");
  const check = helpText("opening.check");
  const post = helpText("opening.post");
  const askChange = helpText("opening.ask-change");
  const booksStart = profile.data?.books_start;

  const set = (account: string, side: keyof Cell, value: string) => {
    setCells({ ...c, [account]: { ...(c[account] ?? { debit: "", credit: "" }), [side]: value } });
    setPreview(null);
  };

  const onSave = async () => {
    if (!current.data) return;
    const { draft, errors: errs } = draftOf(c, base, m);
    setErrors(errs);
    if (!draft) return setStatus("Fix the balances marked, then save again.");
    setBusy(true);
    setProblem(null);
    const etag = current.data.etag;
    const out = await guarded(confirm, () => api.PUT("/v1/opening-balances", { params: { header: { "If-Match": etag } }, body: draft }));
    setBusy(false);
    setFailed("The opening balances were not saved");
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["opening-balances"] });
    setCells(null);
    setMemo(null);
    setStatus("Saved as a draft.");
  };

  const send = async (dryRun: boolean) => {
    if (!current.data) return;
    setBusy(true);
    setProblem(null);
    const etag = current.data.etag;
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/opening-balances/post", { params: { header: dryRun ? { "If-Match": etag, Prefer: "return=preview" } : { "If-Match": etag, "Idempotency-Key": key } } }),
    );
    setBusy(false);
    setFailed(dryRun ? "The opening balances cannot post yet" : "The opening balances were not posted");
    if (!out.data) {
      setPreview(null);
      return setProblem(problemOf(out.error));
    }
    if (dryRun) {
      setPreview(out.data);
      return setStatus("They net to zero and can post. Nothing was saved.");
    }
    setPreview(null);
    await Promise.all(["opening-balances", "journals"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
    setStatus(`Posted at ${date(out.data.entry.posting_date)}.`);
  };

  const ask = async () => {
    if (!current.data) return;
    const { draft, errors: errs } = draftOf(c, base, m);
    setErrors(errs);
    if (!draft) return setStatus("Fix the balances marked, then ask again.");
    if (!reason.trim()) return setReasonMissing(true);
    setBusy(true);
    setProblem(null);
    const etag = current.data.etag;
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/opening-balances/edit-approval", {
        params: { header: { "If-Match": etag, "Idempotency-Key": key } },
        body: { memo: draft.memo, lines: draft.lines, reason: reason.trim() },
      }),
    );
    setBusy(false);
    setFailed("The change was not asked for");
    if (!out.data) return setProblem(problemOf(out.error));
    await Promise.all(["opening-balances", "approvals"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
    setCells(null);
    setMemo(null);
    setChanging(false);
    setReason("");
    setStatus("Asked for approval: the balances change once another person approves.");
  };

  const cancel = () => {
    setChanging(false);
    setCells(null);
    setMemo(null);
    setReason("");
    setReasonMissing(false);
    setErrors({});
  };

  const actions = asking ? (
    <>
      <Button variant="ghost" onClick={cancel} disabled={busy}>
        <Icon of={X} />
        Cancel
      </Button>
      <Button variant="primary" onClick={() => void ask()} disabled={busy} title={askChange.tooltip}>
        <Icon of={Send} />
        {askChange.label}
      </Button>
    </>
  ) : editable && (
    <>
      {(dirty || o?.status === "none") && (
        <Button variant="primary" onClick={() => void onSave()} disabled={busy} title={save.tooltip}>
          <Icon of={Save} />
          {save.label}
        </Button>
      )}
      {!dirty && o?.status === "draft" && (
        <Button onClick={() => void send(true)} disabled={busy} title={check.tooltip}>
          <Icon of={Eye} />
          {check.label}
        </Button>
      )}
      {!dirty && o?.status === "draft" && mayPost && (
        <Button variant="primary" onClick={() => void send(false)} disabled={busy} title={post.tooltip}>
          <Icon of={Send} />
          {post.label}
        </Button>
      )}
    </>
  );
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting">Accounting</Link>
          </nav>
          <div className="fy-row fy-row--wrap gap-2">
            <h1 className="fy-h1">{booksStart ? `Opening balances at ${date(booksStart)}` : "Opening balances"}</h1>
            {posted && (
              <span className="fy-chip fy-chip--neutral">
                <Icon of={Lock} />
                Posted
              </span>
            )}
          </div>
          <p className="fy-secondary">
            {o && (o.prepared_by || o.posted_by)
              ? [o.prepared_by && `Prepared by ${who(o.prepared_by)}`, o.posted_by && `posted by ${who(o.posted_by)}${o.posted_at ? ` on ${dateTime(o.posted_at)}` : ""}`].filter(Boolean).join(" · ")
              : "Each account's balance on the day the books start, so the books carry on from where they were."}
          </p>
        </div>
        <div className="fy-pagehead__actions fy-from-medium">{actions}</div>
      </div>
      <ReviewBanner screen="opening-balances" />
      <p className="fy-small" role="status">
        {status}
      </p>
      {current.isError && <Refusal problem={problemOf(current.error)} title="The opening balances could not be shown" />}
      {problem && <Refusal problem={problem} title={failed} />}
      {profile.data && !booksStart && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={CalendarRange} size={24} />
          <h2 className="fy-h3">Set when the books start first</h2>
          <p className="fy-secondary">The opening balances are dated on the day the books start. Enter it in the organisation's profile.</p>
          <Link className="fy-btn fy-btn--primary" to="/settings/organisation">
            Go to Settings › Organisation
          </Link>
        </div>
      )}
      {accounts.data && chart.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <h2 className="fy-h3">No accounts yet</h2>
          <p className="fy-secondary">Set up the chart of accounts first.</p>
          <Link className="fy-btn fy-btn--primary" to="/accounting/chart-of-accounts">
            Go to the chart of accounts
          </Link>
        </div>
      )}
      {o?.edit_asked && (
        <div className="fy-banner fy-banner--info" role="note">
          <Icon of={Clock} size={20} />
          <div className="fy-banner__text">
            <strong>A change to the opening balances waits for approval</strong>
            <span className="fy-small">Another person who approves journals approves it; until then, the balances stay as posted.</span>
          </div>
          <div className="fy-banner__action">
            <Link className="fy-btn fy-btn--secondary fy-btn--sm" to="/approvals/$approvalId" params={{ approvalId: o.edit_asked }}>
              See the request
            </Link>
          </div>
        </div>
      )}
      {posted && !o?.edit_asked && !asking && (
        <div className="fy-banner fy-banner--info" role="note">
          <Icon of={Lock} size={20} />
          <div className="fy-banner__text">
            <strong>Posted opening balances change only with another person's approval</strong>
            <span className="fy-small">{mayPost ? "Correct them and ask: nothing changes until another person who approves journals approves it." : "An Accountant asks for a change, and another person approves it."}</span>
          </div>
          {mayPost && (
            <div className="fy-banner__action">
              <Button variant="secondary" size="sm" onClick={() => setChanging(true)}>
                <Icon of={PencilLine} />
                Change the balances
              </Button>
            </div>
          )}
        </div>
      )}
      {booksStart && current.data && chart.length > 0 && (
        <section className="fy-card" aria-labelledby={`${id}-accounts`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-accounts`}>
              Accounts
            </h2>
            {editing && (
              <label className="fy-row gap-2 fy-small">
                <input type="checkbox" checked={all} onChange={(e) => setAll(e.target.checked)} />
                Show revenue and expense accounts
              </label>
            )}
          </div>
          {editing && (
            <div className="fy-card__body fy-stack fy-stack--3">
              <div className="fy-field">
                <label className="fy-label" htmlFor={`${id}-memo`}>
                  Where the balances come from
                </label>
                <input className="fy-input" id={`${id}-memo`} value={m} maxLength={1000} placeholder="Such as the trial balance at the end of the last year" onChange={(e) => setMemo(e.target.value)} />
              </div>
              {asking && (
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-reason`}>
                    Why they change
                  </label>
                  <textarea
                    className="fy-textarea"
                    id={`${id}-reason`}
                    value={reason}
                    maxLength={500}
                    onChange={(e) => {
                      setReason(e.target.value);
                      setReasonMissing(false);
                    }}
                    aria-invalid={reasonMissing || undefined}
                    aria-describedby={reasonMissing ? `${id}-reason-error` : undefined}
                  />
                  {reasonMissing && (
                    <span className="fy-error" id={`${id}-reason-error`}>
                      Say why: the approver reads it, and the reversal keeps it.
                    </span>
                  )}
                </div>
              )}
            </div>
          )}
          <div className="fy-tablewrap">
            <table className="fy-table fy-table--compact" aria-labelledby={`${id}-accounts`}>
              <thead>
                <tr>
                  <th scope="col">Account</th>
                  <th scope="col" className="fy-num">
                    Debit ({base})
                  </th>
                  <th scope="col" className="fy-num">
                    Credit ({base})
                  </th>
                </tr>
              </thead>
              {types.map(([type, label]) => {
                const rows = editing ? shown.filter((a) => a.type === type) : (o?.lines ?? []).filter((l) => byId.get(l.account_id)?.type === type).map((l) => byId.get(l.account_id) as Account);
                if (rows.length === 0) return null;
                return (
                  <tbody key={type}>
                    <tr className="fy-group">
                      <td colSpan={3}>{label}</td>
                    </tr>
                    {rows.map((a) => {
                      const cell = c[a.id] ?? { debit: "", credit: "" };
                      return (
                        <tr key={a.id}>
                          <td>
                            <span className="fy-mono">{a.code}</span> {a.name}
                            {errors[a.id] && (
                              <span className="fy-error block" id={`${id}-${a.id}-error`}>
                                {errors[a.id]}
                              </span>
                            )}
                          </td>
                          {editing ? (
                            <>
                              <td className="fy-num">
                                <input
                                  className="fy-input fy-num w-32 medium:w-40"
                                  inputMode="decimal"
                                  aria-label={`${a.code} ${a.name}: debit`}
                                  value={cell.debit}
                                  onChange={(e) => set(a.id, "debit", e.target.value)}
                                  aria-invalid={errors[a.id] ? true : undefined}
                                  aria-describedby={errors[a.id] ? `${id}-${a.id}-error` : undefined}
                                />
                              </td>
                              <td className="fy-num">
                                <input
                                  className="fy-input fy-num w-32 medium:w-40"
                                  inputMode="decimal"
                                  aria-label={`${a.code} ${a.name}: credit`}
                                  value={cell.credit}
                                  onChange={(e) => set(a.id, "credit", e.target.value)}
                                  aria-invalid={errors[a.id] ? true : undefined}
                                  aria-describedby={errors[a.id] ? `${id}-${a.id}-error` : undefined}
                                />
                              </td>
                            </>
                          ) : (
                            <>
                              <td className="fy-num">{cell.debit ? formatAmount(cell.debit) : ""}</td>
                              <td className="fy-num">{cell.credit ? formatAmount(cell.credit) : ""}</td>
                            </>
                          )}
                        </tr>
                      );
                    })}
                  </tbody>
                );
              })}
              <tfoot>
                <tr>
                  <td>Total</td>
                  <td className="fy-num">{formatAmount(t.debits)}</td>
                  <td className="fy-num">{formatAmount(t.credits)}</td>
                </tr>
              </tfoot>
            </table>
          </div>
          <div className="fy-card__foot fy-row fy-row--wrap gap-2">
            {isZero(t.difference) ? (
              <span className="fy-chip fy-chip--good">
                <Icon of={CircleCheck} />
                Balanced
              </span>
            ) : (
              <span className="fy-chip fy-chip--critical">
                <Icon of={CircleX} />
                {sign(t.difference) > 0 ? "Debits exceed credits" : "Credits exceed debits"} by {formatAmount(t.difference.replace(/^-/, ""))} {base}
              </span>
            )}
            {editing && <span className="fy-small fy-muted">Accounts left empty are left out. The difference, if any, usually belongs in 3100 Retained earnings.</span>}
          </div>
        </section>
      )}
      {preview && (
        <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby={`${id}-preview`}>
          <h2 className="fy-h3" id={`${id}-preview`}>
            Ready to post
          </h2>
          <p className="fy-secondary">
            {(preview.entry.lines ?? []).length} lines post on {date(preview.entry.posting_date)}, as entered. A check saves nothing.
          </p>
        </section>
      )}
      {editing && <div className="fy-actionbar fy-only-compact">{actions}</div>}
    </div>
  );
}
