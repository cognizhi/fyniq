import { useQueryClient } from "@tanstack/react-query";
import { Check, Copy, Fingerprint, ShieldCheck, X } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import * as passkey from "../../passkey";
import { CodeNoPasskey, guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { objectLabel, operationLabel, stepWho, type Approval } from "./words";

type Outcome = { data: Approval } | { problem: ApiProblem } | { cancelled: true };
type Decision = "approve" | "reject";

function send(decision: Decision, a: Approval, etag: string, key: string, body: { envelope_hash: string; reason?: string; passkey?: { challenge_id: string; credential: Record<string, unknown> } }) {
  const params = { path: { approval_id: a.id }, header: { "If-Match": etag, "Idempotency-Key": key } };
  return decision === "approve" ? api.POST("/v1/approvals/{approval_id}/approve", { params, body }) : api.POST("/v1/approvals/{approval_id}/reject", { params, body });
}

// decide signs a decision over the envelope the person was shown (FR-APR-03, FR-APR-10). With a
// passkey, the device signs a challenge that carries the envelope's hash (rev 0.44); without one
// (none registered, or a browser that cannot), the session's step-up signs, and the person confirms
// it's you when the API asks. The same Idempotency-Key covers the retry.
export async function decide(decision: Decision, a: Approval, etag: string, confirm: () => Promise<boolean>, reason?: string): Promise<Outcome> {
  const key = newKey();
  const body = { envelope_hash: a.envelope_hash, ...(reason ? { reason } : {}) };
  if (passkey.supported()) {
    const ch = await api.POST("/v1/approvals/{approval_id}/challenge", { params: { path: { approval_id: a.id }, header: { "If-Match": etag } } });
    if (ch.data) {
      let credential: Record<string, unknown>;
      try {
        credential = await passkey.get(ch.data.options);
      } catch (e) {
        if (passkey.cancelled(e)) return { cancelled: true };
        return { problem: { detail: e instanceof Error ? e.message : "Your browser refused the passkey.", fix: "Try again, or use another browser." } };
      }
      const out = await send(decision, a, etag, key, { ...body, passkey: { challenge_id: ch.data.challenge_id, credential } });
      return out.data ? { data: out.data } : { problem: problemOf(out.error) };
    }
    const p = problemOf(ch.error);
    if (p.code !== CodeNoPasskey) return { problem: p };
  }
  const out = await guarded(confirm, () => send(decision, a, etag, key, body));
  return out.data ? { data: out.data } : { problem: problemOf(out.error) };
}

// HashLine shows the envelope's SHA-256 in mono, with a copy action: what the signature covers.
export function HashLine({ hash }: { hash: string }) {
  const [copied, setCopied] = useState(false);
  const h = helpText("approval.copy-hash");
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(hash);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };
  return (
    <div className="fy-row fy-row--wrap gap-2">
      <code className="fy-mono fy-small min-w-0 break-all">{hash}</code>
      <Button variant="ghost" size="sm" onClick={() => void copy()} title={h.tooltip}>
        <Icon of={copied ? Check : Copy} />
        {copied ? "Copied" : h.label}
      </Button>
    </div>
  );
}

// Changes lists what a request changes, before and after (FR-APR-02).
export function Changes({ diff }: { diff: Approval["diff"] }) {
  const rows = diff ?? [];
  if (rows.length === 0) return <p className="fy-small fy-secondary">Nothing to compare: the request names what it changes in its summary.</p>;
  return (
    <div className="fy-tablewrap">
      <table className="fy-table">
        <thead>
          <tr>
            <th scope="col">What</th>
            <th scope="col">Before</th>
            <th scope="col">After</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((d) => (
            <tr key={d.field}>
              <th scope="row" className="fy-strong">
                {d.field}
              </th>
              <td className="fy-secondary">{d.before || "(none)"}</td>
              <td className="fy-strong">{d.after || "(none)"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// ConfirmDecision is the approval confirmation (docs/design/screens ApprovalConfirm): exactly what
// the person signs, the policy step, the envelope's hash, and the decision, signed with a passkey or
// the session's step-up. Approve is never the default focus, and the Inbox never approves directly
// (FR-UX-03): this dialog is the only way.
export function ConfirmDecision({ approval: a, etag, decision, onClose, onDecided }: { approval: Approval; etag: string; decision: Decision | null; onClose: () => void; onDecided: (a: Approval) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const reasonId = useId();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [note, setNote] = useState("");
  const [reason, setReason] = useState("");
  const [reasonError, setReasonError] = useState("");
  const approve = helpText("approval.approve");
  const reject = helpText("approval.reject");
  const withPasskey = passkey.supported();
  const steps = a.steps ?? [];
  const step = steps[a.current_step - 1];

  const submit = async (e?: FormEvent) => {
    e?.preventDefault();
    if (!decision) return;
    setProblem(null);
    setNote("");
    const why = reason.trim();
    if (decision === "reject" && (!why || why.length > 500)) {
      setReasonError("Say why you reject it, in up to 500 characters: the person who asked sees it.");
      return;
    }
    setReasonError("");
    setBusy(true);
    const out = await decide(decision, a, etag, confirm, decision === "reject" ? why : undefined);
    setBusy(false);
    if ("cancelled" in out) {
      setNote("The passkey was cancelled, or took too long. Nothing was signed; try again when you are ready.");
      return;
    }
    if ("problem" in out) {
      setProblem(out.problem);
      return;
    }
    await Promise.all(["approvals", "approval", "review-log", "shell"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
    onDecided(out.data);
  };

  const isReject = decision === "reject";
  return (
    <Dialog
      open={decision !== null}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      wide
      eyebrow={isReject ? "Confirm rejection" : "Confirm approval"}
      title={`${isReject ? "Reject" : "Approve"}: ${a.summary}`}
      description="You are signing exactly what is listed here. If anything in it changes before the change is made, the request becomes void and nothing changes."
      closeLabel="Close without deciding"
      icon={
        <span className="fy-list__icon fy-list__icon--brand">
          <Icon of={ShieldCheck} size={20} />
        </span>
      }
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant={isReject ? "destructive" : "primary"} onClick={() => void submit()} disabled={busy}>
            <Icon of={withPasskey ? Fingerprint : isReject ? X : Check} />
            {busy ? "Signing…" : `${isReject ? reject.label : approve.label}${withPasskey ? " with passkey" : ""}`}
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title={isReject ? "The request was not rejected" : "The request was not approved"} />}
      {note && (
        <p className="fy-small fy-secondary" role="status">
          {note}
        </p>
      )}
      <section className="fy-card fy-card--sunken fy-card--pad" aria-labelledby="ap-what">
        <h3 className="fy-h3 mb-3" id="ap-what">
          What you are {isReject ? "rejecting" : "approving"}
        </h3>
        <dl className="fy-dl fy-dl--3">
          <div>
            <dt>Record</dt>
            <dd>
              {objectLabel(a.object_type)} <span className="fy-mono">{a.object_key}</span>, version {a.version}
            </dd>
          </div>
          <div>
            <dt>Action</dt>
            <dd>{operationLabel(a.operation)}</dd>
          </div>
          <div>
            <dt>Asked for by</dt>
            <dd>
              {a.requested_by_name}, {dateTime(a.requested_at)}
            </dd>
          </div>
          <div>
            <dt>Your step</dt>
            <dd>{step ? `Step ${a.current_step} of ${steps.length}: ${stepWho(step)}` : "Every step has its approvals"}</dd>
          </div>
          <div>
            <dt>Expires</dt>
            <dd>{dateTime(a.expires_at)}</dd>
          </div>
        </dl>
      </section>
      <section className="fy-stack fy-stack--2" aria-labelledby="ap-changes">
        <h3 className="fy-h3" id="ap-changes">
          What it changes
        </h3>
        <Changes diff={a.diff} />
      </section>
      <section className="fy-stack fy-stack--2" aria-labelledby="ap-hash">
        <h3 className="fy-h3" id="ap-hash">
          What your signature covers
        </h3>
        <p className="fy-small fy-secondary">The fingerprint (SHA-256) of the request's envelope. {withPasskey ? "Your passkey signs it." : "Your confirmation signs it."}</p>
        <HashLine hash={a.envelope_hash} />
      </section>
      {isReject && (
        <form className="fy-field" onSubmit={submit} noValidate>
          <label className="fy-label" htmlFor={reasonId}>
            Why you reject it <span className="fy-req">*</span>
          </label>
          <textarea
            className="fy-textarea"
            id={reasonId}
            value={reason}
            maxLength={500}
            onChange={(e) => setReason(e.target.value)}
            aria-invalid={reasonError ? true : undefined}
            aria-describedby={`${reasonId}-help${reasonError ? ` ${reasonId}-error` : ""}`}
          />
          <span className="fy-help" id={`${reasonId}-help`}>
            The person who asked sees it on the request.
          </span>
          {reasonError && (
            <span className="fy-error" id={`${reasonId}-error`}>
              {reasonError}
            </span>
          )}
        </form>
      )}
    </Dialog>
  );
}
