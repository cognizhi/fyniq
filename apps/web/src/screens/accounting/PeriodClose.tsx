import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { CircleCheck, CircleX, Clock, Lock, LockOpen, RefreshCw, TriangleAlert } from "lucide-react";
import { useId, useState } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { who } from "./Journals";
import { monthOf } from "./Periods";
import { Checklist } from "./Checklist";
import { Variances } from "./Variances";
import { YearEnd } from "./YearEnd";

type State = components["schemas"]["PeriodCloseState"];
type Item = components["schemas"]["CloseCheckItem"];

// Who runs the close checks (C on "Period close / reopen": the Accountant, the Controller) and who attests and closes
// (A: the Owner, the Controller). The API decides, and refuses the one who ran the checks (FM-4).
const preparers = ["accountant", "controller"];
const attesters = ["owner", "controller"];

// Reopen asks a closed period back open, with a reason: another founder approves the request (FR-GL-08, FM-1).
function Reopen({ open, month, periodId, etag, onClose, onAsked }: { open: boolean; month: string; periodId: string; etag: string; onClose: () => void; onAsked: (approvalId: string) => void }) {
  const confirm = useConfirm();
  const id = useId();
  const [reason, setReason] = useState("");
  const [missing, setMissing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("close.reopen");
  const ask = async () => {
    if (!reason.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/periods/{period_id}/reopen", { params: { path: { period_id: periodId }, header: { "If-Match": etag, "Idempotency-Key": key } }, body: { reason: reason.trim() } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onAsked(out.data.id);
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={`Ask to reopen ${month}?`}
      description="Another founder approves it first. Once reopened, the month takes postings again until it is closed again; the close's snapshot is kept beside the one taken when it reopens."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void ask()} disabled={busy} title={h.tooltip}>
            <Icon of={LockOpen} />
            {h.label}
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The request was not made" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-reason`}>
          Why it reopens
        </label>
        <textarea
          className="fy-textarea"
          id={`${id}-reason`}
          value={reason}
          maxLength={500}
          onChange={(e) => {
            setReason(e.target.value);
            setMissing(false);
          }}
          aria-invalid={missing || undefined}
          aria-describedby={missing ? `${id}-reason-error` : undefined}
        />
        {missing && (
          <span className="fy-error" id={`${id}-reason-error`}>
            Say why: the approver reads it, and the reopening keeps it.
          </span>
        )}
      </div>
    </Dialog>
  );
}

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
// confirming it's them. The period then refuses every posting, and the close keeps its snapshot's hash. The variances
// against the previous month are explained beside the checks (FR-GL-14), and the month's checklist is worked through
// above them (FR-GL-07). Reconciliations and year end arrive with their tasks.
export function PeriodClose() {
  const { periodId } = useParams({ strict: false }) as { periodId: string };
  const { session } = useSignedIn();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const roles = session.roles ?? [];
  const mayCheck = roles.some((r) => preparers.includes(r));
  const mayAttest = roles.some((r) => attesters.includes(r));
  const mayAsk = roles.some((r) => preparers.includes(r));
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
  const [reopening, setReopening] = useState(false);
  const reopen = helpText("close.reopen");
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
      {s?.reopen_asked && (
        <div className="fy-banner fy-banner--info" role="note">
          <Icon of={Clock} size={20} />
          <div className="fy-banner__text">
            <strong>A request to reopen {month} waits for approval</strong>
            <span className="fy-small">Another founder approves it; until then, the month stays closed.</span>
          </div>
          <div className="fy-banner__action">
            <Link className="fy-btn fy-btn--secondary fy-btn--sm" to="/approvals/$approvalId" params={{ approvalId: s.reopen_asked }}>
              See the request
            </Link>
          </div>
        </div>
      )}
      {s && (
        <div className="fy-split">
          <div className="fy-stack fy-stack--6">
            <Checklist periodId={periodId} open={open} mayMark={mayCheck} onChanged={setStatus} />
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
            <Variances periodId={periodId} month={month} open={open} mayExplain={mayCheck} onExplained={setStatus} />
          </div>

          <aside className="fy-stack fy-stack--6" aria-label="Attestation and year end">
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
            {s.reopen && (
              <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby={`${id}-reopened`}>
                <h2 className="fy-h3" id={`${id}-reopened`}>
                  Reopened
                </h2>
                <p className="fy-small">
                  Approved by {who(s.reopen.reopened_by)} on {dateTime(s.reopen.reopened_at)}: “{s.reopen.reason}”
                </p>
                <dl className="fy-dl">
                  <div>
                    <dt>Before</dt>
                    <dd className="fy-mono">sha256 {s.reopen.before_sha256.slice(0, 4)}…{s.reopen.before_sha256.slice(-4)}</dd>
                  </div>
                  <div>
                    <dt>After</dt>
                    <dd className="fy-mono">sha256 {s.reopen.after_sha256.slice(0, 4)}…{s.reopen.after_sha256.slice(-4)}</dd>
                  </div>
                </dl>
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
                {!open && !s.reopen_asked && mayAsk && (
                  <>
                    <Button className="self-start" variant="ghost" size="sm" onClick={() => setReopening(true)} title={reopen.tooltip}>
                      <Icon of={LockOpen} />
                      {reopen.label}…
                    </Button>
                    <p className="fy-small fy-muted">Reopening needs another founder's approval and a reason, and keeps snapshots before and after.</p>
                  </>
                )}
              </section>
            )}
            <YearEnd periodId={periodId} />
          </aside>
        </div>
      )}
      {s && q.data && (
        <Reopen
          open={reopening}
          month={month}
          periodId={periodId}
          etag={q.data.etag}
          onClose={() => setReopening(false)}
          onAsked={async () => {
            setReopening(false);
            await refresh();
            setStatus(`Asked to reopen ${month}: another founder approves it.`);
          }}
        />
      )}
    </div>
  );
}
