import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ChevronRight, ClipboardCheck, ClockAlert, Inbox as InboxIcon, Keyboard, Stamp } from "lucide-react";
import { useState, type KeyboardEvent } from "react";
import { api } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { date, dateTime } from "../../format/date";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";
import { DeadlinesSoon } from "../compliance/DeadlinesSoon";
import { recorders } from "../compliance/StatutoryCalendar";
import { BillsToPay } from "../purchases/BillsBoard";
import { operationLabel, StatusChip, type Approval } from "./words";

// Who records bill payments: P on "Bills, vendor credits" (Appendix D), the Accountant; the Inbox lists them the bills to
// pay soon (FR-AP-13). The API decides what they may do.
const payers = ["accountant"];

async function listApprovals(status: "pending" | "all") {
  const { data, error } = await api.GET("/v1/approvals", { params: { query: { status, page_size: 200 } } });
  if (error) throw error;
  return data.items ?? [];
}

async function reviewSummary() {
  const { data, error } = await api.GET("/v1/review-log", { params: { query: { status: "unreviewed", page_size: 100 } } });
  if (error) throw error;
  return data;
}

function Row({ a, mine }: { a: Approval; mine: boolean }) {
  return (
    <li>
      <Link className="fy-list__item" to="/approvals/$approvalId" params={{ approvalId: a.id }} data-approval={a.id} data-decides={a.status === "pending" && a.you_may_approve}>
        <span className={a.you_may_approve ? "fy-list__icon fy-list__icon--brand" : "fy-list__icon"}>
          <Icon of={Stamp} size={20} />
        </span>
        <span className="fy-list__main">
          <span className="fy-list__title whitespace-normal">{a.summary}</span>
          <span className="fy-list__meta whitespace-normal">
            {operationLabel(a.operation)} · asked for by {mine ? "you" : a.requested_by_name}, {dateTime(a.requested_at)}
            {a.status === "pending" ? ` · expires ${date(a.expires_at)}` : ""}
          </span>
        </span>
        {a.status === "pending" && a.you_may_approve ? <span className="fy-chip fy-chip--info">Your turn</span> : <StatusChip status={a.status} />}
        <Icon of={ChevronRight} />
      </Link>
    </li>
  );
}

function Group({ id, title, items, empty, mine }: { id: string; title: string; items: Approval[]; empty: string; mine: (a: Approval) => boolean }) {
  return (
    <section className="fy-card" aria-labelledby={id}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={id}>
          {title}
        </h2>
        <span className="fy-badge fy-badge--quiet">{items.length}</span>
      </div>
      {items.length === 0 ? (
        <p className="fy-card__body fy-small fy-secondary">{empty}</p>
      ) : (
        <ul className="fy-list fy-stagger" aria-labelledby={id}>
          {items.map((a) => (
            <Row key={a.id} a={a} mine={mine(a)} />
          ))}
        </ul>
      )}
    </section>
  );
}

// The triage keys (FR-UX-03), active only while a request in the list has focus, so they never fire in a text field and
// need no switch to turn them off (WCAG 2.2 SC 2.1.4): J or ↓ and K or ↑ move, E opens, A and R open the request with its
// approve or reject confirmation showing, which alone decides; ? shows them.
const triageKeys = [
  ["J or ↓", "Next request"],
  ["K or ↑", "Previous request"],
  ["E or Enter", "Open it"],
  ["A", "Open its approval to confirm"],
  ["R", "Open its rejection to confirm"],
  ["?", "Show or hide these keys"],
] as const;

// Inbox (docs/design/screens Inbox; FR-UX-03, FR-APR-01, FM-3): what waits for you. Today its
// groups are the approval requests (yours to decide, yours waiting, others') and the weekly review
// of high-risk actions; AI proposals, checks, mentions and deadlines join with their modules. A
// request opens on its own page, whose Approve opens the confirmation: nothing here decides.
export function Inbox() {
  const { session } = useSignedIn();
  const [all, setAll] = useState(false);
  const approvals = useQuery({ queryKey: ["approvals", all ? "all" : "pending"], queryFn: () => listApprovals(all ? "all" : "pending") });
  const review = useQuery({ queryKey: ["review-log", "summary"], queryFn: reviewSummary });
  const mine = (a: Approval) => a.requested_by === session.user_id;
  const items = approvals.data ?? [];
  const pending = items.filter((a) => a.status === "pending");
  const toYou = pending.filter((a) => a.you_may_approve);
  const yours = pending.filter((a) => !a.you_may_approve && mine(a));
  const others = pending.filter((a) => !a.you_may_approve && !mine(a));
  const decided = items.filter((a) => a.status !== "pending");
  const toReview = (review.data?.items ?? []).filter((e) => e.you_may_review).length;
  const navigate = useNavigate();
  const [keys, setKeys] = useState(false);
  const [said, setSaid] = useState("");
  const triage = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.altKey || e.ctrlKey || e.metaKey) return;
    const links = Array.from(e.currentTarget.querySelectorAll<HTMLAnchorElement>("a[data-approval]"));
    const at = links.indexOf(document.activeElement as HTMLAnchorElement);
    if (at < 0) return;
    const here = links[at];
    const move = (to: number) => links[Math.max(0, Math.min(links.length - 1, to))]?.focus();
    switch (e.key) {
      case "j":
      case "ArrowDown":
        move(at + 1);
        break;
      case "k":
      case "ArrowUp":
        move(at - 1);
        break;
      case "e":
        here?.click();
        break;
      case "a":
      case "r":
        if (here?.dataset.decides !== "true") {
          setSaid("This request is not yours to decide: E opens it.");
          break;
        }
        void navigate({ to: "/approvals/$approvalId", params: { approvalId: here.dataset.approval ?? "" }, search: { decide: e.key === "a" ? "approve" : "reject" } });
        break;
      case "?":
        setKeys(!keys);
        break;
      default:
        return;
    }
    e.preventDefault();
  };

  return (
    <div className="fy-page fy-page--narrow fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">Inbox</h1>
          <p className="fy-secondary">What waits for you: requests to approve, your own requests, and the weekly review.</p>
        </div>
      </div>
      <p className="fy-sr" role="status" aria-live="polite">
        {said || (approvals.data ? `${toYou.length} ${toYou.length === 1 ? "approval waits" : "approvals wait"} for you` : "")}
      </p>
      {approvals.isError && <Refusal problem={problemOf(approvals.error)} title="The requests could not be listed" />}
      {approvals.isPending && <p className="fy-secondary">Loading the requests…</p>}
      {approvals.data && (
        <div className="fy-stack fy-stack--4" onKeyDown={triage}>
          <Group id="to-approve" title="To approve" items={toYou} mine={mine} empty="Nothing waits for your approval." />
          <Group id="your-requests" title="Your requests, waiting" items={yours} mine={mine} empty="None of your requests is waiting." />
          {others.length > 0 && <Group id="others" title="Waiting for others" items={others} mine={mine} empty="" />}
        </div>
      )}
      {approvals.data && pending.length > 0 && (
        <div className="fy-stack fy-stack--2">
          <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm self-start" aria-expanded={keys} onClick={() => setKeys(!keys)}>
            <Icon of={Keyboard} />
            Keyboard: J and K move, E opens, A and R confirm
          </button>
          {keys && (
            <dl className="fy-small fy-grid fy-grid--2 gap-1" aria-label="Inbox keys">
              {triageKeys.map(([key, what]) => (
                <div key={key} className="fy-row gap-2">
                  <dt className="fy-mono">{key}</dt>
                  <dd>{what}</dd>
                </div>
              ))}
            </dl>
          )}
          {said && <p className="fy-small fy-secondary">{said}</p>}
        </div>
      )}
      {(session.roles ?? []).some((r) => payers.includes(r)) && <BillsToPay />}
      {(session.roles ?? []).some((r) => recorders.includes(r)) && <DeadlinesSoon />}
      {review.data && (
        <section className="fy-card" aria-labelledby="weekly-review">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="weekly-review">
              Weekly review
            </h2>
            {review.data.overdue > 0 && (
              <span className="fy-chip fy-chip--serious">
                <Icon of={ClockAlert} />
                {review.data.overdue} overdue
              </span>
            )}
          </div>
          <Link className="fy-list__item" to="/review-log">
            <span className="fy-list__icon">
              <Icon of={ClipboardCheck} size={20} />
            </span>
            <span className="fy-list__main">
              <span className="fy-list__title whitespace-normal">{review.data.unreviewed === 0 ? "Every high-risk action is reviewed" : `${review.data.unreviewed} high-risk ${review.data.unreviewed === 1 ? "action" : "actions"} not reviewed yet`}</span>
              <span className="fy-list__meta whitespace-normal">{toReview > 0 ? `${toReview} you can review: another founder's actions.` : "Another founder reviews the actions you took."}</span>
            </span>
            <Icon of={ChevronRight} />
          </Link>
        </section>
      )}
      {all && decided.length > 0 && <Group id="decided" title="Decided" items={decided} mine={mine} empty="" />}
      <div>
        <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" onClick={() => setAll(!all)} aria-pressed={all}>
          <Icon of={InboxIcon} />
          {all ? "Hide decided requests" : "Show decided requests"}
        </button>
      </div>
    </div>
  );
}
