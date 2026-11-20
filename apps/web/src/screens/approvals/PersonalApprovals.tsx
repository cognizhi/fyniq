import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRight, CircleCheck, Stamp } from "lucide-react";
import { api } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";
import type { Approval } from "./words";

async function claims(status: "pending" | "approved") {
  const { data, error } = await api.GET("/v1/approvals", { params: { query: { status, page_size: 200 } } });
  if (error) throw error;
  return (data.items ?? []).filter((a) => a.operation === "expense_claim.post");
}

// PersonalApprovals is To approve, where a department Approver approves the claims of the departments their role is scoped to (docs/design/screens PersonalApprovals; FR-UX-39, FR-AP-17, FR-APR-02, FR-APR-03; owner's
// decision of 21 Nov 2026: by department scope, no line-manager routing, the Accountant and Controller as the fallback). The API decides who sees and who approves a claim; this lists what it answers and opens each in the
// approval page, which shows what approving posts and takes the signed decision. Nobody approves their own claim, and a claim outside a person's departments is never listed for them.
export function PersonalApprovals() {
  const { session } = useSignedIn();
  const h = helpText("approvals.personal");
  const pending = useQuery({ queryKey: ["personal-approvals", "pending"], queryFn: () => claims("pending") });
  const done = useQuery({ queryKey: ["personal-approvals", "approved"], queryFn: () => claims("approved") });
  const mine = (a: Approval) => a.requested_by === session.user_id;
  const yours = (pending.data ?? []).filter((a) => a.you_may_approve && !mine(a));
  const waiting = (pending.data ?? []).filter((a) => !a.you_may_approve || mine(a));
  const decidedByYou = (done.data ?? []).filter((a) => (a.signatures ?? []).some((s) => s.signer === session.user_id && s.decision === "approve")).slice(0, 10);
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1" title={h.tooltip}>
            {h.label}
          </h1>
          <p className="fy-secondary">Expense claims waiting for you to approve. You see a claim here when its department is one your role covers, or when your role approves every claim.</p>
        </div>
      </div>
      <ReviewBanner screen="personal-approvals" />
      {pending.isError && <Refusal problem={problemOf(pending.error)} title="The claims could not be shown" />}
      <section aria-label="Waiting for you" className="fy-stack fy-stack--2">
        <h2 className="fy-h2">Waiting for you</h2>
        {pending.data && yours.length === 0 && <p className="fy-secondary">Nothing to approve.</p>}
        <ul className="fy-list">
          {yours.map((a) => (
            <li key={a.id}>
              <Link className="fy-list__item" to="/approvals/$approvalId" params={{ approvalId: a.id }}>
                <span className="fy-list__icon fy-list__icon--brand">
                  <Icon of={Stamp} size={20} />
                </span>
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">{a.summary}</span>
                  <span className="fy-list__meta whitespace-normal">
                    Claimed by {a.requested_by_name}, {dateTime(a.requested_at)} · expires {date(a.expires_at)}
                  </span>
                </span>
                <span className="fy-chip fy-chip--info">Your turn</span>
                <Icon of={ChevronRight} />
              </Link>
            </li>
          ))}
        </ul>
      </section>
      {waiting.length > 0 && (
        <section aria-label="Not yours to approve" className="fy-stack fy-stack--2">
          <h2 className="fy-h2">Not yours to approve</h2>
          <ul className="fy-stack fy-stack--1">
            {waiting.map((a) => (
              <li key={a.id}>
                {a.summary}: {mine(a) ? "your own claim, which another person approves" : "outside the departments your role covers"}
              </li>
            ))}
          </ul>
        </section>
      )}
      <section aria-label="Recently approved by you" className="fy-stack fy-stack--2">
        <h2 className="fy-h2">Recently approved by you</h2>
        {decidedByYou.length === 0 ? (
          <p className="fy-secondary">None yet.</p>
        ) : (
          <ul className="fy-stack fy-stack--1">
            {decidedByYou.map((a) => (
              <li key={a.id}>
                <Icon of={CircleCheck} /> {a.summary}
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
