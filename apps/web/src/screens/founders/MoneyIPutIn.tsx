import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Fingerprint, HandCoins, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { api, newKey } from "../../api/client";
import { Button } from "../../components/ui/button";
import { CodeStepUpRequired, stepUp } from "../../stepup";
import type { ApiProblem } from "../../ui";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { isZero, sign } from "../../format/decimal";
import { Icon, problemOf } from "../../ui";
import { originLabel } from "./data";
import { owed, YearEndLine } from "./DirectorAccounts";

// Confirm asks the founder to confirm their balances at the latest financial year end, with their passkey (FR-EQ-12):
// the API keeps the balances the ledger holds at that end now.
function Confirm({ yearEnd, onDone }: { yearEnd: string; onDone: (said: string) => void }) {
  const queries = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const send = () => api.POST("/v1/me/director-account/confirmations", { params: { header: { "Idempotency-Key": key } }, body: { year_end: yearEnd } });
  const confirm = async () => {
    setBusy(true);
    setProblem(null);
    let out = await send();
    const code = out.error ? problemOf(out.error).code : "";
    if (code === "FOUNDER-26" || code === CodeStepUpRequired) {
      const s = await stepUp();
      if (!s.ok) {
        setBusy(false);
        return setProblem(s.problem ?? { detail: s.cancelled ? "The passkey was cancelled." : "Your browser has no passkey for Fyniq.", fix: "Add a passkey in Preferences › Security, then try again." });
      }
      out = await send();
    }
    setBusy(false);
    if (out.error) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["my-director-account"] });
    onDone(`Confirmed your balances at ${date(yearEnd)}.`);
  };
  return (
    <div className="fy-stack fy-stack--2">
      {problem && <Refusal problem={problem} title="Your balances were not confirmed" />}
      <Button variant="primary" size="sm" type="button" onClick={confirm} disabled={busy}>
        <Icon of={Fingerprint} />
        {busy ? "Confirming…" : `Confirm my balances at ${date(yearEnd)} with my passkey`}
      </Button>
    </div>
  );
}

// latest are a founder's movements of the last 90 days, when their roles may read a statement; null otherwise (an
// Employee's "V (own)" reads their balances only).
async function latest(contact: string, until: string) {
  const from = new Date(`${until}T00:00:00Z`);
  from.setUTCDate(from.getUTCDate() - 90);
  const r = await api.GET("/v1/director-accounts/{contact_id}/statement", {
    params: { path: { contact_id: contact }, query: { from: from.toISOString().slice(0, 10), to: until } },
  });
  if (r.error) return null;
  return [...(r.data.movements ?? [])].reverse().slice(0, 8);
}

// MoneyIPutIn is a founder's own view, You › Money I put in (docs/design/screens MoneyIPutIn; FR-EQ-14, FR-EQ-01):
// what the company owes them on their director account, in plain words, and the latest amounts by where they came
// from. Basic: the settlements and the register of share issues are Expert's.
export function MoneyIPutIn() {
  const q = useQuery({
    queryKey: ["my-director-account"],
    queryFn: async () => {
      const r = await api.GET("/v1/me/director-account");
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const a = q.data?.account;
  const [said, setSaid] = useState("");
  const items = useQuery({ queryKey: ["my-director-latest", a?.contact_id], enabled: !!a, queryFn: () => latest(a?.contact_id ?? "", new Date().toISOString().slice(0, 10)) });
  return (
    <div className="fy-page fy-page--narrow fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">Money I put in</h1>
          <p className="fy-secondary">What the company owes you for money you lent, costs you paid and pay or fees not yet paid, and where each amount came from.</p>
        </div>
      </div>
      <ReviewBanner screen="money-i-put-in" />
      {q.isError && <Refusal problem={problemOf(q.error)} title="Your account could not be shown" />}
      {q.isPending && <p className="fy-secondary">Loading your account…</p>}
      {q.data && !q.data.linked && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={HandCoins} size={24} />
          <h2 className="fy-h3">No director account is linked to you</h2>
          <p className="fy-secondary">If you are a director or shareholder, ask an Owner to link your contact to you in Accounting › Director accounts. Another person approves the link.</p>
        </div>
      )}
      {a && (
        <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby="mine-title">
          <h2 className="fy-h3" id="mine-title">
            {isZero(a.net.amount) ? "Nothing owed either way" : sign(a.net.amount) < 0 ? "The company owes you" : "You owe the company"}
          </h2>
          {!isZero(a.net.amount) && (
            <p className="fy-num fy-strong" style={{ fontSize: "var(--fs-xl)" }}>
              {formatAmount(owed(a.net.amount).replace(/^-/, ""))} {a.net.currency}
            </p>
          )}
          <dl className="fy-dl">
            {(a.balances ?? []).map((b) => (
              <div key={b.account_id}>
                <dt>{b.role === "director-loan" ? "Money lent and costs you paid" : "Pay and fees not yet paid"}</dt>
                <dd className="fy-num">{formatAmount(owed(b.balance.amount))}</dd>
              </div>
            ))}
          </dl>
          {a.in_debit && (
            <p className="fy-row fy-row--wrap gap-2">
              <span className="fy-chip fy-chip--warning">
                <Icon of={TriangleAlert} />
                In debit
              </span>
              <span className="fy-small">You owe the company. {a.debit_warning ?? "A loan to a director is restricted unless the company is an exempt private company: repay it, or ask your accountant."}</span>
            </p>
          )}
          {a.year_end && <YearEndLine p={a} />}
          {a.year_end && a.year_end.status !== "confirmed" && <Confirm yearEnd={a.year_end.year_end} onDone={setSaid} />}
          {said && (
            <p className="fy-small" role="status">
              {said}
            </p>
          )}
          <p className="fy-small fy-muted">
            Paid for something yourself? <Link to="/me/claims/new">Claim it</Link>, and keep it as a loan to the company.
          </p>
        </section>
      )}
      {items.data && items.data.length > 0 && (
        <section className="fy-card" aria-labelledby="latest-title">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="latest-title">
              Latest
            </h2>
          </div>
          <ul className="fy-list">
            {items.data.map((m, i) => (
              <li key={`${m.entry_id}-${i}`} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">{originLabel(m.origin)}</span>
                  <span className="fy-list__meta whitespace-normal">{[date(m.date), m.description || m.memo].filter(Boolean).join(" · ")}</span>
                </span>
                <span className="fy-num">{formatAmount(owed(m.amount.amount))}</span>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
