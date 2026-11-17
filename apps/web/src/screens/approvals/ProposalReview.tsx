import { useQuery } from "@tanstack/react-query";
import { Link, Navigate, useParams } from "@tanstack/react-router";
import { ChevronLeft, FileText, ShieldCheck, Sparkles } from "lucide-react";
import { useEffect, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf } from "../../ui";
import { PaperView, type PaperShape } from "../compliance/CorporatePaper";

type Proposal = components["schemas"]["AIProposalDetail"];

async function getProposal(id: string): Promise<Proposal> {
  const r = await api.GET("/v1/ai/proposals/{proposal_id}", { params: { path: { proposal_id: id } } });
  if (r.error) throw r.error;
  return r.data;
}

const tierWords: Record<number, string> = { 1: "Tier 1 · routine", 2: "Tier 2 · posts or changes records", 3: "Tier 3" };

// Value shows a part of a payload, an item or an impact as the task type reports it: words for its fields, never
// figures worked out here (FR-UX-22).
function Value({ v }: { v: unknown }) {
  if (v === null || v === undefined) return <span className="fy-muted">none</span>;
  if (Array.isArray(v))
    return (
      <ul className="fy-list fy-list--plain">
        {v.map((x, i) => (
          <li key={i}>
            <Value v={x} />
          </li>
        ))}
      </ul>
    );
  if (typeof v === "object")
    return (
      <dl className="fy-dl fy-dl--compact">
        {Object.entries(v as Record<string, unknown>).map(([k, x]) => (
          <div key={k}>
            <dt>{k.replace(/_/g, " ")}</dt>
            <dd>
              <Value v={x} />
            </dd>
          </div>
        ))}
      </dl>
    );
  return <span>{String(v)}</span>;
}

// papersOf are the corporate documents a sec.draft_document dry run would draft, each with its text, when it reports
// them: shown as their papers read, not as fields.
function papersOf(impact: unknown): { paper: PaperShape }[] | null {
  const docs = (impact as { corporate_documents?: { paper?: PaperShape }[] } | null)?.corporate_documents;
  if (!Array.isArray(docs) || !docs.every((d) => d && typeof d.paper?.title === "string")) return null;
  return docs as { paper: PaperShape }[];
}

// ProposalReview is an AI proposal as its approver reviews it (AI-07; design D5, D10; docs/design/screens ProposalReview):
// Fyniq's summary from the proposal and its dry run, never the AI's words; the items to keep, each selected to start;
// what changes and the evidence; and the AI's reasons apart, in violet with the AI label (MASTER.md: violet only for AI
// content). onSelect hears which items the approval covers: null for the whole of a tier-1 proposal, else the items
// kept, which a proposal above tier 1 always names (AI-32).
export function ProposalReview({ proposalId, onSelect }: { proposalId: string; onSelect?: (items: number[] | null) => void }) {
  const q = useQuery({ queryKey: ["ai-proposal", proposalId], queryFn: () => getProposal(proposalId) });
  const [kept, setKept] = useState<number[] | null>(null);
  const items = helpText("ai-proposal.items");
  const reasons = helpText("ai-proposal.rationale");
  const p = q.data;
  const all = (p?.item_list ?? []).map((it) => it.n);
  const keep = kept ?? all;
  useEffect(() => {
    if (!p || !onSelect) return;
    const whole = keep.length === all.length;
    onSelect(whole && p.tier === 1 ? null : keep);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- keep and all follow p and kept
  }, [p, kept]);
  if (q.isPending) return <p className="fy-secondary">Loading the proposal…</p>;
  if (q.isError) return <Refusal problem={problemOf(q.error)} title="The proposal could not be shown" />;
  if (!p) return null;
  const toggle = (n: number) => setKept(keep.includes(n) ? keep.filter((x) => x !== n) : [...keep, n].sort((a, b) => a - b));

  return (
    <div className="fy-stack fy-stack--6">
      <header className="fy-card fy-card--pad fy-stack fy-stack--3">
        <div className="fy-row fy-row--wrap">
          <span className="fy-ai-badge">
            <Icon of={Sparkles} />
            AI
          </span>
          <span className="fy-chip fy-chip--neutral">
            <Icon of={ShieldCheck} />
            {tierWords[p.tier] ?? `Tier ${p.tier}`}
          </span>
        </div>
        <p className="fy-small fy-muted">
          <span className="fy-mono fy-expert-only">
            {p.task_type} v{p.task_version} ·{" "}
          </span>
          {p.task_name} · proposed by the AI {dateTime(p.proposed_at)} · expires {dateTime(p.expires_at)}
        </p>
      </header>

      <section className="fy-card" aria-labelledby="proposal-summary">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="proposal-summary">
            Summary
          </h2>
        </div>
        <div className="fy-card__body fy-stack fy-stack--3">
          <span className="fy-small fy-muted">Written by Fyniq from the proposal and its dry run, not by the AI</span>
          <p>{p.summary || "Fyniq has no summary of this proposal."}</p>
          {p.reason && <p className="fy-small fy-secondary">{p.reason}</p>}
        </div>
      </section>

      {p.item_list.length > 0 && (
        <section className="fy-card" aria-labelledby="proposal-items">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="proposal-items" title={items.tooltip}>
              {items.label}
            </h2>
            <span className="fy-small fy-muted" role="status">
              {keep.length} of {all.length} kept
            </span>
          </div>
          <div className="fy-card__body fy-stack fy-stack--3">
            <p className="fy-small fy-secondary">
              {p.tier === 1
                ? "Every item starts kept. Clear any you disagree with; the approval then covers only the items you keep."
                : "Above tier 1 each item you approve is named: clear any you disagree with."}
            </p>
            <ul className="fy-list">
              {p.item_list.map((it) => (
                <li key={it.n} className="fy-list__item">
                  <label className="fy-row">
                    <input type="checkbox" checked={keep.includes(it.n)} onChange={() => toggle(it.n)} disabled={!onSelect} aria-label={`Keep item ${it.n}`} />
                    <span className="fy-strong">Item {it.n}</span>
                  </label>
                  <Value v={it.item} />
                </li>
              ))}
            </ul>
            {keep.length === 0 && <p className="fy-small fy-error">Keep at least one item, or reject the proposal.</p>}
          </div>
        </section>
      )}

      <section className="fy-card" aria-labelledby="proposal-changes">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="proposal-changes">
            What changes
          </h2>
        </div>
        <div className="fy-card__body">
          {papersOf(p.impact) ? (
            <div className="fy-stack fy-stack--3">
              {papersOf(p.impact)?.map((d, i) => <PaperView key={i} paper={d.paper} />)}
            </div>
          ) : p.impact ? (
            <Value v={p.impact} />
          ) : (
            <p className="fy-secondary">Its dry run reported no detail.</p>
          )}
        </div>
      </section>

      <section className="fy-card" aria-labelledby="proposal-evidence">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="proposal-evidence">
            Evidence
          </h2>
        </div>
        <div className="fy-card__body">
          {p.evidence.length === 0 ? (
            <p className="fy-secondary">The AI named no record it relied on.</p>
          ) : (
            <ul className="fy-list">
              {p.evidence.map((e) => (
                <li key={`${e.kind}:${e.id}`} className="fy-list__item">
                  <Icon of={FileText} />
                  <span>{e.title || e.id}</span>
                  <span className="fy-small fy-muted">
                    {e.kind} <span className="fy-mono">{e.id}</span>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </section>

      {p.rationale && (
        <section className="fy-ai-card" aria-labelledby="proposal-reasons">
          <div className="fy-ai-card__head">
            <span className="fy-ai-badge">
              <Icon of={Sparkles} />
              AI
            </span>
            <h2 className="fy-h3" id="proposal-reasons" title={reasons.tooltip}>
              {reasons.label}
            </h2>
          </div>
          <div className="fy-ai-card__body">
            <p className="fy-ai-text">{p.rationale.text}</p>
            <p className="fy-small fy-muted">The AI wrote this. Check it against Fyniq's summary above, which says what really happens.</p>
          </div>
        </section>
      )}

      <p className="fy-small fy-muted fy-expert-only">
        {p.agent} on <span className="fy-mono">{p.model}</span>, prompt <span className="fy-mono">{p.prompt}</span>; payload{" "}
        <span className="fy-mono">{p.payload_sha256}</span>
      </p>
    </div>
  );
}

// ProposalPage is /inbox/proposals/$proposalId: a proposal waiting for a decision opens its approval request, where it is
// reviewed and decided; one with none (invalid, or decided) is shown as it is.
export function ProposalPage() {
  const { proposalId } = useParams({ strict: false }) as { proposalId: string };
  const q = useQuery({ queryKey: ["ai-proposal", proposalId], queryFn: () => getProposal(proposalId) });
  const h = helpText("ai-proposal.review");
  if (q.data?.approval_id && q.data.status === "pending") return <Navigate to="/approvals/$approvalId" params={{ approvalId: q.data.approval_id }} replace />;
  return (
    <div className="fy-page fy-page--narrow fy-enter">
      <Link className="fy-btn fy-btn--ghost fy-btn--sm -ms-2 self-start" to="/inbox">
        <Icon of={ChevronLeft} />
        Inbox
      </Link>
      <h1 className="fy-h1" title={h.tooltip}>
        {h.label}
      </h1>
      {q.isError ? <Refusal problem={problemOf(q.error)} title="The proposal could not be shown" /> : <ProposalReview proposalId={proposalId} />}
    </div>
  );
}
