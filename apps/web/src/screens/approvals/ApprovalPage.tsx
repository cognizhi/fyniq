import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { Check, ChevronLeft, CircleCheck, CircleX, Fingerprint, Undo2, X } from "lucide-react";
import { useState } from "react";
import { api, etagOf, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { Changes, ConfirmDecision, HashLine } from "./Decide";
import { objectLabel, operationLabel, StatusChip, stepWho, type Approval } from "./words";
import { EvidencePanel } from "../accounting/JournalEvidence";
import { CompareNumbers } from "../contacts/BankDetails";

async function getApproval(id: string) {
  const r = await api.GET("/v1/approvals/{approval_id}", { params: { path: { approval_id: id } } });
  if (r.error) throw r.error;
  return { approval: r.data, etag: etagOf(r.response) || `"${r.data.version}"` };
}

function Steps({ a }: { a: Approval }) {
  const steps = a.steps ?? [];
  return (
    <ol className="fy-steps fy-steps--vertical">
      {steps.map((s, i) => {
        const n = i + 1;
        const done = s.approvals >= s.quorum;
        const current = a.status === "pending" && n === a.current_step;
        return (
          <li key={n} className={done ? "fy-step fy-step--done" : "fy-step"} aria-current={current ? "step" : undefined}>
            <span className="fy-step__n">{done ? <Icon of={Check} /> : n}</span>
            <span className="fy-stack gap-0">
              <span className="fy-strong">
                Step {n}
                {current ? ", waiting" : done ? ", done" : ""}
              </span>
              <span className="fy-small fy-secondary">
                {stepWho(s)} · {s.approvals} of {s.quorum} so far
              </span>
            </span>
          </li>
        );
      })}
    </ol>
  );
}

function Signatures({ a }: { a: Approval }) {
  const sigs = a.signatures ?? [];
  if (sigs.length === 0) return <p className="fy-small fy-secondary">Nobody has signed it yet.</p>;
  return (
    <ul className="fy-list rounded-md border border-border">
      {sigs.map((s) => (
        <li key={`${s.signer}-${s.signed_at}`} className="fy-list__item">
          <span className={s.decision === "approve" ? "fy-list__icon fy-list__icon--good" : "fy-list__icon fy-list__icon--critical"}>
            <Icon of={s.decision === "approve" ? CircleCheck : CircleX} size={20} />
          </span>
          <span className="fy-list__main">
            <span className="fy-list__title whitespace-normal">
              {s.signer_name} {s.decision === "approve" ? "approved" : "rejected"}
              {s.voided ? " (void)" : ""}
            </span>
            <span className="fy-list__meta whitespace-normal">
              {dateTime(s.signed_at)} · {s.method === "passkey" ? "signed with a passkey" : "confirmed at sign-in"}
              {s.reason ? ` · "${s.reason}"` : ""}
            </span>
          </span>
          {s.method === "passkey" && <Icon of={Fingerprint} />}
        </li>
      ))}
    </ul>
  );
}

function Withdraw({ a, etag, open, onClose }: { a: Approval; etag: string; open: boolean; onClose: () => void }) {
  const queries = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("approval.withdraw");
  const run = async () => {
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/approvals/{approval_id}/withdraw", { params: { path: { approval_id: a.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } } });
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    await Promise.all(["approvals", "approval"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
    onClose();
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title="Withdraw your request?"
      description="It becomes void with any approvals it has, and nothing changes. Ask again later if you still want the change."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Keep it
          </Button>
          <Button variant="destructive" onClick={run} disabled={busy}>
            <Icon of={Undo2} />
            {h.label}
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The request was not withdrawn" />}
    </Dialog>
  );
}

// ApprovalPage is one approval request (docs/design/screens Inbox detail, ApprovalConfirm): what it
// changes, who approves it in which step, the signatures so far, and the envelope's hash; Approve
// and Reject open the confirmation, never deciding at once (FR-UX-03). The requester may withdraw.
export function ApprovalPage() {
  const { approvalId } = useParams({ strict: false }) as { approvalId: string };
  const { session } = useSignedIn();
  const q = useQuery({ queryKey: ["approval", approvalId], queryFn: () => getApproval(approvalId) });
  const [decision, setDecision] = useState<"approve" | "reject" | null>(null);
  const [withdrawing, setWithdrawing] = useState(false);
  const [done, setDone] = useState("");
  const approve = helpText("approval.approve");
  const reject = helpText("approval.reject");
  const withdraw = helpText("approval.withdraw");

  const back = (
    <Link className="fy-btn fy-btn--ghost fy-btn--sm -ms-2 self-start" to="/inbox">
      <Icon of={ChevronLeft} />
      Inbox
    </Link>
  );
  if (q.isPending)
    return (
      <div className="fy-page fy-page--narrow">
        {back}
        <p className="fy-secondary">Loading the request…</p>
      </div>
    );
  if (q.isError)
    return (
      <div className="fy-page fy-page--narrow">
        {back}
        <Refusal problem={problemOf(q.error)} title="The request could not be shown" />
      </div>
    );
  const { approval: a, etag } = q.data;
  const mine = a.requested_by === session.user_id;
  const pending = a.status === "pending";

  return (
    <div className="fy-page fy-page--narrow fy-enter">
      {back}
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">
            {objectLabel(a.object_type)} · {operationLabel(a.operation)}
          </span>
          <h1 className="fy-h1">{a.summary}</h1>
          <p className="fy-secondary">
            Asked for by {mine ? "you" : a.requested_by_name} on {dateTime(a.requested_at)}
            {pending ? ` · expires ${dateTime(a.expires_at)}` : a.decided_at ? ` · decided ${dateTime(a.decided_at)}` : ""}
          </p>
          {a.object_type === "journal" && (
            <p className="fy-small">
              <Link to="/accounting/journals/$journalId" params={{ journalId: a.object_key }}>
                Open the journal and its lines
              </Link>
            </p>
          )}
          {a.object_type === "payee_bank_change" && (
            <p className="fy-small fy-secondary">Compare each number with the vendor's letter in the change's evidence before you approve; payments to the vendor are frozen while it waits.</p>
          )}
          {a.object_type === "period" && (
            <p className="fy-small">
              <Link to="/accounting/close/$periodId" params={{ periodId: a.object_key }}>
                Open the period and its close
              </Link>
            </p>
          )}
        </div>
        <div className="fy-pagehead__actions">
          <StatusChip status={a.status} />
        </div>
      </div>

      <p className="fy-small" role="status">
        {done}
      </p>
      {a.status_reason && (
        <div className="fy-banner fy-banner--info" role="note">
          <div className="fy-banner__text">
            <span className="fy-strong">Why it is {a.status}</span>
            <span className="fy-small">{a.status_reason}</span>
          </div>
        </div>
      )}
      {pending && (a.you_may_approve || mine) && (
        <div className="fy-row fy-row--wrap">
          {a.you_may_approve && (
            <>
              <Button variant="primary" onClick={() => setDecision("approve")} title={approve.tooltip}>
                <Icon of={Check} />
                {approve.label}…
              </Button>
              <Button onClick={() => setDecision("reject")} title={reject.tooltip}>
                <Icon of={X} />
                {reject.label}…
              </Button>
            </>
          )}
          {mine && (
            <Button variant="ghost" onClick={() => setWithdrawing(true)} title={withdraw.tooltip}>
              <Icon of={Undo2} />
              {withdraw.label}…
            </Button>
          )}
        </div>
      )}
      {pending && !a.you_may_approve && !mine && <p className="fy-small fy-secondary">This request waits for someone its current step names. You can follow it here.</p>}

      <section className="fy-card" aria-labelledby="changes-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="changes-title">
            What it changes
          </h2>
        </div>
        <div className="fy-card__body">
          <Changes diff={a.diff} />
        </div>
      </section>

      {a.object_type === "payee_bank_change" && (
        <>
          <CompareNumbers changeId={a.object_key} envelope={a.envelope} />
          <EvidencePanel record={{ kind: "bank-change", id: a.object_key, etag: "" }} draft={false} mayAttach={false} onChanged={() => undefined} />
        </>
      )}

      <section className="fy-card" aria-labelledby="who-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="who-title">
            Who approves it
          </h2>
          <span className="fy-badge fy-badge--quiet fy-expert-only">{a.policy}</span>
        </div>
        <div className="fy-card__body fy-stack fy-stack--3">
          <Steps a={a} />
          <h3 className="fy-h3">Signatures</h3>
          <Signatures a={a} />
        </div>
      </section>

      <section className="fy-card" aria-labelledby="sign-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="sign-title">
            What a signature covers
          </h2>
        </div>
        <div className="fy-card__body fy-stack fy-stack--2">
          <p className="fy-small fy-secondary">Each approval signs this fingerprint (SHA-256) of the request's envelope: change anything in it, and the signature no longer matches.</p>
          <HashLine hash={a.envelope_hash} />
          <details className="fy-expert-only">
            <summary className="fy-small">The envelope (RFC 8785 canonical JSON)</summary>
            <pre className="fy-mono fy-small mt-2 overflow-x-auto whitespace-pre-wrap break-all">{a.envelope}</pre>
          </details>
        </div>
      </section>

      <ConfirmDecision
        approval={a}
        etag={etag}
        decision={decision}
        onClose={() => setDecision(null)}
        onDecided={(after) => {
          setDecision(null);
          setDone(after.status === "approved" ? "Approved: the change is made." : after.status === "rejected" ? "Rejected: nothing changes." : "Signed: the request waits for its next step.");
        }}
      />
      <Withdraw a={a} etag={etag} open={withdrawing} onClose={() => setWithdrawing(false)} />
    </div>
  );
}
