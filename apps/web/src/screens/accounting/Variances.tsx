import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, MessageSquareText, TriangleAlert } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { who } from "./Journals";
import { monthOf } from "./Periods";

type Page = components["schemas"]["VariancePage"];
type Variance = components["schemas"]["Variance"];

// percentOf shows the change's percentage as the API rounded it (RND-PERCENT), with its sign: "+60.0%", "−40.0%".
function percentOf(v: Variance): string {
  if (v.change_percent === undefined) return "new";
  const p = v.change_percent;
  return p.startsWith("-") ? `−${p.slice(1)}%` : p === "0.0" ? "0.0%" : `+${p}%`;
}

// Explain asks why an account moved, for the two totals the page shows: the API refuses them once either changed
// (CLOSE-10), and a recorded explanation goes stale when they change (FR-GL-14).
function Explain({ v, periodId, months, onClose, onDone }: { v: Variance; periodId: string; months: [string, string]; onClose: () => void; onDone: () => void }) {
  const confirm = useConfirm();
  const id = useId();
  const [words, setWords] = useState(v.explanation && !v.explained ? v.explanation.explanation : "");
  const [missing, setMissing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("close.explain-variance");
  const send = async () => {
    if (!words.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/periods/{period_id}/variances/{account_id}/explanation", {
        params: { path: { period_id: periodId, account_id: v.account_id }, header: { "Idempotency-Key": key } },
        body: { current: v.current, previous: v.previous, explanation: words.trim() },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone();
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={`Explain ${v.code} ${v.name}`}
      description={`${months[0]}: ${formatAmount(v.previous.amount)}; ${months[1]}: ${formatAmount(v.current.amount)} ${v.current.currency}. The explanation is of these totals: if either changes, the close asks for a new one.`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void send()} disabled={busy} title={h.tooltip}>
            <Icon of={MessageSquareText} />
            {h.label}
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The explanation was not saved" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-words`}>
          Why it moved
        </label>
        <textarea
          className="fy-textarea"
          id={`${id}-words`}
          value={words}
          maxLength={1000}
          onChange={(e) => {
            setWords(e.target.value);
            setMissing(false);
          }}
          aria-invalid={missing || undefined}
          aria-describedby={missing ? `${id}-words-error` : undefined}
        />
        {missing && (
          <span className="fy-error" id={`${id}-words-error`}>
            Say why the account moved: the founder who attests the close reads it.
          </span>
        )}
      </div>
    </Dialog>
  );
}

function Status({ v }: { v: Variance }) {
  const e = v.explanation;
  if (v.explained && e) {
    return (
      <>
        <span className="fy-chip fy-chip--good">
          <Icon of={CircleCheck} />
          Explained
        </span>{" "}
        <span className="fy-small">
          {e.explanation} · {who(e.written_by)}
        </span>
      </>
    );
  }
  if (e) {
    return (
      <>
        <span className="fy-chip fy-chip--warning">
          <Icon of={TriangleAlert} />
          The totals changed
        </span>{" "}
        <span className="fy-small fy-muted">
          Explained for {formatAmount(e.previous.amount)} and {formatAmount(e.current.amount)}: “{e.explanation}”
        </span>
      </>
    );
  }
  if (v.needs_explanation) {
    return (
      <span className="fy-chip fy-chip--warning">
        <Icon of={TriangleAlert} />
        Needs an explanation
      </span>
    );
  }
  return <span className="fy-small fy-muted">Within the thresholds</span>;
}

// Variances is the close's flux analysis (FR-GL-14, Appendix C.2 #57; docs/design/screens PeriodClose): each income or
// expense account against the previous month, those above both thresholds first in importance, each with its latest
// explanation. The members who run the checks explain them while the month is open; the checks then see them once run
// again. The AI's draft arrives with the AI catalogue, budget variances with budgets.
export function Variances({ periodId, month, open, mayExplain, onExplained }: { periodId: string; month: string; open: boolean; mayExplain: boolean; onExplained: (message: string) => void }) {
  const queries = useQueryClient();
  const id = useId();
  const [all, setAll] = useState(false);
  const [explaining, setExplaining] = useState<Variance | null>(null);
  const q = useQuery({
    queryKey: ["period-variances", periodId],
    queryFn: async () => {
      const r = await api.GET("/v1/periods/{period_id}/variances", { params: { path: { period_id: periodId }, query: { page_size: 500 } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const p: Page | undefined = q.data;
  const before = p?.previous_period ? monthOf(p.previous_period) : "";
  const listed = (p?.items ?? []).filter((v) => all || v.needs_explanation || v.explanation);
  const hidden = (p?.items.length ?? 0) - listed.length;
  const percent = p ? `${p.threshold_percent}%` : "";
  return (
    <section className="fy-card" aria-labelledby={`${id}-variances`}>
      <div className="fy-card__head" style={{ flexWrap: "wrap" }}>
        <h2 className="fy-h3" id={`${id}-variances`}>
          Variances to explain
        </h2>
        {p?.previous_period && (
          <span className="fy-small fy-muted">
            More than {p.threshold_amount.currency} {formatAmount(p.threshold_amount.amount)} and {percent} against {before}
          </span>
        )}
      </div>
      {q.isError && <Refusal problem={problemOf(q.error)} title="The variances could not be shown" />}
      {p && !p.previous_period && <p className="fy-card__body fy-secondary">No earlier month to compare: {month} has no variances to explain.</p>}
      {p?.previous_period && (
        <>
          <p className="fy-card__body fy-small">
            {p.unexplained === 0
              ? "Every variance above the thresholds is explained."
              : p.unexplained === 1
                ? "1 variance needs an explanation before the close."
                : `${p.unexplained} variances need an explanation before the close.`}
          </p>
          {listed.length > 0 && (
            <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
              <table className="fy-table fy-table--cards">
                <caption className="fy-sr">
                  Income and expense accounts, {before} against {month}, in {p.threshold_amount.currency}
                </caption>
                <thead>
                  <tr>
                    <th scope="col">Account</th>
                    <th scope="col" className="fy-num fy-from-medium">
                      {before}
                    </th>
                    <th scope="col" className="fy-num fy-from-medium">
                      {month}
                    </th>
                    <th scope="col" className="fy-num">
                      Change
                    </th>
                    <th scope="col">Explanation</th>
                  </tr>
                </thead>
                <tbody>
                  {listed.map((v) => (
                    <tr key={v.account_id}>
                      <td data-card="title">
                        <span className="fy-acct">{v.code}</span>
                        {v.name}
                      </td>
                      <td data-card="hide" className="fy-num fy-from-medium">
                        {formatAmount(v.previous.amount)}
                      </td>
                      <td data-card="hide" className="fy-num fy-from-medium">
                        {formatAmount(v.current.amount)}
                      </td>
                      <td data-card="amount" className="fy-num">
                        {formatAmount(v.change.amount)} <span className="fy-small fy-muted">{percentOf(v)}</span>
                      </td>
                      <td data-card="meta">
                        <Status v={v} />
                        {open && mayExplain && (v.needs_explanation || v.explanation) && !v.explained && (
                          <div className="fy-row" style={{ marginTop: "var(--space-2)" }}>
                            <Button
                              variant="secondary"
                              size="sm"
                              onClick={() => setExplaining(v)}
                              title={helpText("close.explain-variance").tooltip}
                              aria-label={`${helpText("close.explain-variance").label} ${v.code} ${v.name}`}
                            >
                              <Icon of={MessageSquareText} />
                              {helpText("close.explain-variance").label}
                            </Button>
                          </div>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {(hidden > 0 || all) && (
            <div className="fy-card__body">
              <Button variant="ghost" size="sm" onClick={() => setAll(!all)} aria-expanded={all}>
                {all ? "Show only the variances to explain" : `Show every income and expense account (${p.items.length})`}
              </Button>
            </div>
          )}
          {p.next_cursor && <p className="fy-card__body fy-small fy-muted">Showing the first 500 accounts by code.</p>}
        </>
      )}
      {explaining && p?.previous_period && (
        <Explain
          v={explaining}
          periodId={periodId}
          months={[before, month]}
          onClose={() => setExplaining(null)}
          onDone={async () => {
            setExplaining(null);
            await queries.invalidateQueries({ queryKey: ["period-variances", periodId] });
            onExplained(`Explained ${explaining.code} ${explaining.name}. Run the checks again so they see it.`);
          }}
        />
      )}
    </section>
  );
}
