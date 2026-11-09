import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRight, CircleCheck, CircleMinus, ClockAlert, Hourglass, TriangleAlert, type LucideIcon } from "lucide-react";
import { useId } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";
import { PoliciesToAcknowledge } from "./Policies";

type Review = components["schemas"]["ComplianceReview"];

async function reviews() {
  const { data, error } = await api.GET("/v1/compliance/reviews");
  if (error) throw error;
  return data.items;
}

// Who reads the reviews: everyone with a right on Appendix D's "Compliance Center".
const readers = ["owner", "administrator", "controller", "accountant", "auditor", "dpo"];

const owners: Record<Review["owner"], string> = { owner: "Owner", controller: "Controller", accountant: "Accountant", dpo: "Compliance officer", member: "Every member" };

const chips: Record<Review["status"], { chip: string; icon: LucideIcon; label: string }> = {
  overdue: { chip: "fy-chip--serious", icon: ClockAlert, label: "Overdue" },
  due: { chip: "fy-chip--warning", icon: Hourglass, label: "Due" },
  signed: { chip: "fy-chip--good", icon: CircleCheck, label: "Signed off" },
  nothing_due: { chip: "fy-chip--neutral", icon: CircleMinus, label: "Nothing due" },
  not_applicable: { chip: "fy-chip--neutral", icon: CircleMinus, label: "Not needed" },
};

// ReviewChip is a review's status, as an icon and a label.
export function ReviewChip({ status }: { status: Review["status"] }) {
  const c = chips[status];
  return (
    <span className={`fy-chip ${c.chip}`}>
      <Icon of={c.icon} />
      {c.label}
    </span>
  );
}

// Overdue for more than 14 days past its due date: called out with a warning (the screen's "⚠ older than 14 days").
function longOverdue(r: Review, today: string): boolean {
  if (r.status !== "overdue" || !r.due_on) return false;
  const due = new Date(`${r.due_on}T00:00:00Z`);
  due.setUTCDate(due.getUTCDate() + 14);
  return due.toISOString().slice(0, 10) < today;
}

// Attestations is Compliance › Reviews and attestations (CMP-03, FR-CTL-06, SEC-06, FM-3; p1-compliance-center D6): the
// recurring reviews people sign off, overdue first, each with its owner role, period, due date, last sign-off and
// evidence, linking to where it is signed off. Policy acknowledgements are signed off here.
export function Attestations() {
  const id = useId();
  const h = helpText("compliance.attestations");
  const { mode } = useSignedIn();
  const list = useQuery({ queryKey: ["compliance-reviews"], queryFn: reviews });
  const today = new Date().toISOString().slice(0, 10);
  const rank: Record<Review["status"], number> = { overdue: 0, due: 1, signed: 2, nothing_due: 3, not_applicable: 4 };
  const all = (list.data ?? []).slice().sort((a, b) => rank[a.status] - rank[b.status]);
  const overdue = all.filter((r) => r.status === "overdue");
  return (
    <div className="fy-page fy-enter">
      <nav className="fy-breadcrumb" aria-label="Breadcrumb">
        <Link to="/compliance">Compliance</Link>
        <Icon of={ChevronRight} />
        <span>{h.label}</span>
      </nav>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">{h.label}</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
      </div>
      <PoliciesToAcknowledge />
      {list.isError && <Refusal problem={problemOf(list.error)} title="The reviews could not be shown" />}
      {list.isPending && <p className="fy-small">Loading the reviews…</p>}
      {overdue.length > 0 && (
        <div className="fy-banner fy-banner--warning" role="status">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <strong>
              {overdue.length} review{overdue.length === 1 ? " is" : "s are"} overdue
            </strong>
            <span className="fy-small">Each is signed off on its own page: follow its link.</span>
          </div>
        </div>
      )}
      {list.data && (
        <section className="fy-card" aria-labelledby={`${id}-reviews`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-reviews`}>
              Recurring reviews
            </h2>
            <span className="fy-small fy-muted">Overdue first</span>
          </div>
          <ul className="fy-list" aria-labelledby={`${id}-reviews`}>
            {all.map((r) => (
              <li key={r.kind}>
                <Link className="fy-list__item" to={r.where} aria-label={`${r.title}: ${chips[r.status].label}. Sign off where it is kept`}>
                  <span className="fy-list__main">
                    <span className="fy-list__title whitespace-normal">{r.title}</span>
                    <span className="fy-list__meta whitespace-normal">
                      {owners[r.owner]} · {r.cadence}
                      {r.period && ` · ${r.period}`}
                      {r.due_on && ` · ${r.kind === "risk-review" && r.waiting === 0 ? "next" : "due"} ${date(r.due_on)}`}
                    </span>
                    <span className="fy-list__meta whitespace-normal">{r.detail}</span>
                    <span className="fy-list__meta whitespace-normal">
                      {r.last_sign_off ? `Last signed off ${date(r.last_sign_off.at)}${r.last_sign_off.by_name ? ` by ${r.last_sign_off.by_name}` : ""}` : "Never signed off yet"}
                      {mode === "expert" && ` · evidence: ${r.evidence}`}
                      {mode === "expert" && r.controls.length > 0 && ` · ${r.controls.join(", ")}`}
                    </span>
                  </span>
                  {longOverdue(r, today) && (
                    <span className="fy-chip fy-chip--warning" title="More than 14 days past its due date">
                      <Icon of={TriangleAlert} />
                      Over 14 days
                    </span>
                  )}
                  <ReviewChip status={r.status} />
                  <Icon of={ChevronRight} />
                </Link>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}

// ReviewsDue is the Inbox's list of the reviews due or overdue that the viewer's roles sign off (CMP-03); the policy
// acknowledgements have their own card. Nothing when none waits.
export function ReviewsDue() {
  const { session } = useSignedIn();
  const mine = new Set(session.roles ?? []);
  const list = useQuery({ queryKey: ["compliance-reviews"], queryFn: reviews, enabled: readers.some((r) => mine.has(r)) });
  const due = (list.data ?? []).filter((r) => (r.status === "due" || r.status === "overdue") && r.owner !== "member" && mine.has(r.owner));
  if (due.length === 0) return null;
  return (
    <section className="fy-card" aria-labelledby="reviews-due-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="reviews-due-title" title={helpText("inbox.reviews").tooltip}>
          Reviews to sign off
        </h2>
      </div>
      <ul className="fy-list" aria-labelledby="reviews-due-title">
        {due.map((r) => (
          <li key={r.kind}>
            <Link className="fy-list__item" to={r.where}>
              <span className="fy-list__main">
                <span className="fy-list__title whitespace-normal">{r.title}</span>
                <span className="fy-list__meta whitespace-normal">{r.detail}</span>
              </span>
              <ReviewChip status={r.status} />
              <Icon of={ChevronRight} />
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}
