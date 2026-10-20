import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { CircleCheck, CircleX, Lock, RefreshCw, TriangleAlert } from "lucide-react";
import { useId, useState } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { who } from "./Journals";
import { monthOf } from "./Periods";

type State = components["schemas"]["PeriodCloseState"];
type Item = components["schemas"]["CloseCheckItem"];

// Who runs the close checks (C on "Period close / reopen": the Accountant, the Controller) and who attests and closes
// (A: the Owner, the Controller). The API decides, and refuses the one who ran the checks (FM-4).
const preparers = ["accountant", "controller"];
const attesters = ["owner", "controller"];

function CheckRow({ c }: { c: Item }) {
  const icon = c.severity === "error" ? CircleX : c.severity === "warning" ? TriangleAlert : CircleCheck;
  const tone = c.severity === "error" ? "fy-list__icon--critical" : c.severity === "warning" ? "fy-list__icon--warning" : "fy-list__icon--good";
  return (
    <li className="fy-list__item">
      <span className={`fy-list__icon ${tone}`}>
        <Icon of={icon} size={20} />
      </span>
      <span className="fy-list__main">
        <span className="fy-list__title whitespace-normal">{c.title}</span>
        {c.detail && <span className="fy-list__meta whitespace-normal">{c.detail}</span>}
      </span>
      <span className="fy-sr">{c.severity === "error" ? "Blocks the close" : c.severity === "warning" ? "Review it" : "Done"}</span>
    </li>
  );
}

// PeriodClose closes a month (docs/design/screens PeriodClose; FR-GL-07, FR-GL-08, FM-4, AUD-04): one person runs the
// close checks, which record the ledger revision they saw; another, who did not run them, attests and closes, after
// confirming it's them. The period then refuses every posting, and the close keeps its snapshot's hash. The checklist
// template, reconciliations, flux, reopening and year end arrive with their tasks.
export function PeriodClose() {
  const { periodId } = useParams({ strict: false }) as { periodId: string };
  const { session } = useSignedIn();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const roles = session.roles ?? [];
  const mayCheck = roles.some((r) => preparers.includes(r));
  const mayAttest = roles.some((r) => attesters.includes(r));
  const q = useQuery({
    queryKey: ["period-close", periodId],
    queryFn: async () => {
      const r = await api.GET("/v1/periods/{period_id}/close", { params: { path: { period_id: periodId } } });
      if (r.error) throw r.error;
      return { state: r.data, etag: etagOf(r.response) };
    },
  });
  const s: State | undefined = q.data?.state;
  const month = s ? monthOf(s.period) : "the month";
  const [statement, setStatement] = useState<string | null>(null);
  const text = statement ?? `I have reviewed the close of ${month} and its checks, and the books for the month are complete as far as I know.`;
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [failed, setFailed] = useState("");
  const [status, setStatus] = useState("");
  const run = helpText("close.run-checks");
  const attest = helpText("close.attest");
  const open = s?.period.status === "open";
  const checks = s?.checks;
  const blocking = (checks?.checks ?? []).filter((c) => c.severity === "error").length;

  const refresh = () => Promise.all(["period-close", "periods"].map((k) => queries.invalidateQueries({ queryKey: [k] })));

  const runChecks = async () => {
    if (!q.data) return;
    setBusy(true);
    setProblem(null);
    const etag = q.data.etag;
    const key = newKey();
    const out = await guarded(confirm, () => api.POST("/v1/periods/{period_id}/close-checks", { params: { path: { period_id: periodId }, header: { "If-Match": etag, "Idempotency-Key": key } } }));
    setBusy(false);
    setFailed("The checks did not run");
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh();
    setStatus(out.data.checks?.clean ? "The checks are clean: another founder can attest and close." : "The checks found something that blocks the close.");
  };

  const close = async () => {
    if (!q.data || !checks) return;
    setBusy(true);
    setProblem(null);
    const etag = q.data.etag;
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/periods/{period_id}/close", {
        params: { path: { period_id: periodId }, header: { "If-Match": etag, "Idempotency-Key": key } },
        body: { checks_id: checks.id, statement: text.trim() },
      }),
    );
    setBusy(false);
    setFailed("The period was not closed");
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh();
    setStatus(`${month} is closed.`);
  };

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting">Accounting</Link>
            <span aria-hidden="true">›</span>
            <Link to="/accounting/close">Periods and close</Link>
          </nav>
          <div className="fy-row fy-row--wrap gap-2">
            <h1 className="fy-h1">{open ? `Close ${month}` : month}</h1>
            {s && (open ? <span className="fy-chip fy-chip--good">Open</span> : <span className="fy-chip fy-chip--neutral"><Icon of={Lock} />Closed</span>)}
          </div>
          {checks && (
            <p className="fy-secondary">
              Checks run by {who(checks.checked_by)} on {dateTime(checks.checked_at)}, at ledger revision {checks.ledger_revision}
            </p>
          )}
        </div>
        {open && mayCheck && (
          <div className="fy-pagehead__actions">
            <Button variant={checks ? "secondary" : "primary"} onClick={() => void runChecks()} disabled={busy} title={run.tooltip}>
              <Icon of={RefreshCw} />
              {checks ? "Run the checks again" : run.label}
            </Button>
          </div>
        )}
      </div>
      <ReviewBanner screen="period-close" />
      <p className="fy-small" role="status">
        {status}
      </p>
      {q.isError && <Refusal problem={problemOf(q.error)} title="The close could not be shown" />}
      {problem && <Refusal problem={problem} title={failed} />}
      {open && checks && blocking > 0 && (
        <div className="fy-banner fy-banner--critical" role="note">
          <Icon of={CircleX} size={20} />
          <div className="fy-banner__text">
            <strong>{blocking === 1 ? "1 check blocks the close" : `${blocking} checks block the close`}</strong>
            <span className="fy-small">Fix what they found, then run the checks again.</span>
          </div>
        </div>
      )}
      {open && checks?.stale && (
        <div className="fy-banner fy-banner--warning" role="note">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <strong>The checks are out of date</strong>
            <span className="fy-small">Something dated in or before {month} posted after they ran. Run them again before the close.</span>
          </div>
        </div>
      )}
      {s && (
        <div className="fy-split">
          <section className="fy-card" aria-labelledby={`${id}-checks`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-checks`}>
                Close checks
              </h2>
              {checks && <span className="fy-small fy-muted">{checks.clean ? "Nothing blocks the close" : "The close is blocked"}</span>}
            </div>
            {open && !checks && (
              <p className="fy-card__body fy-secondary">
                {mayCheck ? "Run the checks first. They record the ledger as they find it, so another founder can attest and close." : "An Accountant or a Controller runs the checks first."}
              </p>
            )}
            {checks && (
              <ul className="fy-list" aria-labelledby={`${id}-checks`}>
                {checks.checks.map((c) => (
                  <CheckRow key={c.key} c={c} />
                ))}
              </ul>
            )}
            {!open && <p className="fy-card__body fy-secondary">The period is closed: nothing posts in it, by any path.</p>}
          </section>

          <aside className="fy-stack fy-stack--6" aria-label="Attestation">
            {open && (
              <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-attest`}>
                <h2 className="fy-h3" id={`${id}-attest`}>
                  Attestation
                </h2>
                <p className="fy-small">A founder who did not run the checks confirms {month}, then closes it. Fyniq asks them to confirm it's them first.</p>
                {s.you_may_attest && mayAttest ? (
                  <>
                    <div className="fy-field">
                      <label className="fy-label" htmlFor={`${id}-statement`}>
                        What you attest
                      </label>
                      <textarea className="fy-textarea" id={`${id}-statement`} value={text} maxLength={1000} onChange={(e) => setStatement(e.target.value)} />
                    </div>
                    <Button variant="primary" onClick={() => void close()} disabled={busy || !text.trim()} title={attest.tooltip}>
                      <Icon of={Lock} />
                      {attest.label}
                    </Button>
                  </>
                ) : (
                  <span className="fy-chip fy-chip--neutral self-start">
                    {!checks
                      ? "Waiting for the checks"
                      : !checks.clean
                        ? "Waiting for clean checks"
                        : checks.stale
                          ? "Waiting for the checks to run again"
                          : mayAttest
                            ? "Another founder attests: you ran the checks"
                            : "An Owner or a Controller attests"}
                  </span>
                )}
              </section>
            )}
            {s.close && (
              <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-closed`}>
                <div className="fy-row gap-2">
                  <Icon of={CircleCheck} size={24} />
                  <h2 className="fy-h3" id={`${id}-closed`}>
                    {open ? "Closed before" : "Books closed"}
                  </h2>
                </div>
                <p className="fy-small">
                  Attested by {who(s.close.attested_by)} on {dateTime(s.close.attested_at)}.
                </p>
                <p className="fy-small fy-secondary">“{s.close.statement}”</p>
                <dl className="fy-dl">
                  <div>
                    <dt>Ledger revision</dt>
                    <dd className="fy-mono">{s.close.closed_revision}</dd>
                  </div>
                  <div>
                    <dt>Snapshot</dt>
                    <dd className="fy-mono" title={s.close.snapshot_sha256}>
                      sha256 {s.close.snapshot_sha256.slice(0, 4)}…{s.close.snapshot_sha256.slice(-4)}
                    </dd>
                  </div>
                </dl>
                {!open && <p className="fy-small fy-muted">Reopening needs a second founder's approval and a reason, and keeps snapshots before and after. It arrives soon.</p>}
              </section>
            )}
          </aside>
        </div>
      )}
    </div>
  );
}
