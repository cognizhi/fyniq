import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { Hourglass, Send } from "lucide-react";
import { useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { EvidencePanel } from "../accounting/JournalEvidence";
import { EquityTabs } from "./Holders";
import { blankResolution, ResolutionFields, resolutionBody, useCapital } from "./OptionForms";
import { count, drafters, RecordStatus, resolutionOf, type PoolResolutionDraft } from "./optionWords";

// PoolResolutionNew is Equity › Option pool › Record a resolution (FR-EQ-19): the shares the shareholders set aside
// for options, the class, and the resolution. Saved as a draft, it takes the signed resolution on its own page.
export function PoolResolutionNew() {
  const navigate = useNavigate();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const capital = useCapital();
  const [form, setForm] = useState<PoolResolutionDraft>(blankResolution);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const draftH = helpText("pool-resolution.draft");
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/option-pool/resolutions", { params: { header: { "Idempotency-Key": key } }, body: resolutionBody(form, capital[0]?.id ?? "") }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["option-pool"] });
    await navigate({ to: "/equity/options/resolutions/$resolutionId", params: { resolutionId: out.data.id } });
  };
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/equity/options">Option pool</Link>
            <span aria-hidden="true">›</span>
            <span>Record a resolution</span>
          </nav>
          <h1 className="fy-h1" title={draftH.tooltip}>
            Record a resolution setting the pool
          </h1>
          <p className="fy-secondary">The pool has this size from the day it was passed, until a later resolution sets it again.</p>
        </div>
      </div>
      <EquityTabs current="options" />
      <form className="fy-card fy-card--pad fy-stack fy-stack--4" onSubmit={submit} aria-label="Record a resolution setting the pool">
        {problem && <Refusal problem={problem} title="The resolution was not drafted" />}
        <ResolutionFields form={form} set={setForm} />
        <div className="fy-row justify-end gap-2">
          <Link className="fy-btn fy-btn--ghost" to="/equity/options">
            Cancel
          </Link>
          <Button variant="primary" type="submit" disabled={busy} title={draftH.tooltip}>
            {draftH.label}
          </Button>
        </div>
      </form>
    </div>
  );
}

// PoolResolutionPage is Equity › Option pool › a resolution (FR-EQ-19, FR-DOC-06): its size, class and day, the signed
// resolution as evidence, its approval, and whether it is in force.
export function PoolResolutionPage() {
  const { resolutionId } = useParams({ strict: false }) as { resolutionId: string };
  const { session } = useSignedIn();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const q = useQuery({ queryKey: ["pool-resolution", resolutionId], queryFn: () => resolutionOf(resolutionId) });
  const [status, setStatus] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const mayDraft = (session.roles ?? []).some((r) => drafters.includes(r));
  const askH = helpText("pool-resolution.ask");
  const refresh = async (text: string) => {
    setStatus(text);
    await queries.invalidateQueries({ queryKey: ["pool-resolution", resolutionId] });
    await queries.invalidateQueries({ queryKey: ["option-pool"] });
  };
  if (q.isError) return <Refusal problem={problemOf(q.error)} title="The resolution could not be shown" />;
  if (!q.data) return null;
  const { r, etag } = q.data;
  const ask = async () => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/option-pool/resolutions/{resolution_id}/approval", {
        params: { path: { resolution_id: r.id }, header: { "If-Match": etag, "Idempotency-Key": newKey() } },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await refresh("Asked for approval. Another Owner or the Controller approves it.");
  };
  const rows: [string, string][] = [
    ["Shares set aside", count(r.size)],
    ["Over the class of", r.class_code],
    ["Resolution", r.resolution_ref],
    ["Passed on", date(r.resolved_on)],
    ["In force today", r.in_force ? "Yes" : "No"],
  ];
  if (r.recorded_at) rows.push(["Recorded", `${date(r.recorded_at)}${r.recorded_by ? ` by ${r.recorded_by.name}` : ""}`]);
  if (r.note) rows.push(["Note", r.note]);
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/equity/options">Option pool</Link>
            <span aria-hidden="true">›</span>
            <span>Resolution of {date(r.resolved_on)}</span>
          </nav>
          <h1 className="fy-h1">Pool of {count(r.size)} from {date(r.resolved_on)}</h1>
          <RecordStatus status={r.status} asked={r.approval_id} />
        </div>
        <div className="fy-pagehead__actions">
          {r.status === "draft" && mayDraft && !r.approval_id && (
            <Button onClick={() => void ask()} disabled={busy || r.evidence === 0} title={askH.tooltip}>
              <Icon of={Send} />
              {askH.label}
            </Button>
          )}
          {r.approval_id && (
            <Link className="fy-btn fy-btn--secondary" to="/approvals/$approvalId" params={{ approvalId: r.approval_id }}>
              <Icon of={Hourglass} />
              Waiting for approval
            </Link>
          )}
        </div>
      </div>
      <EquityTabs current="options" />
      <p className="fy-small" role="status">
        {status}
      </p>
      {problem && <Refusal problem={problem} title="That was not done" />}
      <div className="fy-split">
        <section className="fy-card" aria-labelledby="resolution-terms">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="resolution-terms">
              The resolution
            </h2>
          </div>
          <dl className="fy-card__body fy-dl">
            {rows.map(([k, v]) => (
              <div key={k}>
                <dt>{k}</dt>
                <dd>{v}</dd>
              </div>
            ))}
          </dl>
        </section>
        <div className="fy-stack fy-stack--4">
          {r.checks.length > 0 && (
            <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-label="What stops it">
              {r.checks.map((c) => (
                <p key={`${c.code}-${c.field}`} className="fy-small fy-secondary">
                  {c.message}. {c.fix}.
                </p>
              ))}
            </section>
          )}
          <EvidencePanel record={{ kind: "pool-resolution", id: r.id, etag }} draft={r.status === "draft"} mayAttach={mayDraft && r.status === "draft" && !r.approval_id}
            onChanged={(m) => void refresh(m)} />
        </div>
      </div>
    </div>
  );
}
