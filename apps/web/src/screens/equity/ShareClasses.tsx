import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, Hourglass, PencilLine, Plus, Send } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listAccounts } from "../accounting/data";
import { EvidencePanel } from "../accounting/JournalEvidence";
import { EquityTabs } from "./Holders";

type ShareClass = components["schemas"]["ShareClassView"];
type Draft = components["schemas"]["ShareClassDraft"];

// Who drafts a class: C/P on the cap table, the Accountant (Appendix D); an Owner or the Controller approves.
const drafters = ["accountant"];

const antiDilution: Record<string, string> = { none: "None", broad_based: "Broad-based weighted average", full_ratchet: "Full ratchet" };
const dividends: Record<string, string> = { none: "None", non_cumulative: "Non-cumulative", cumulative: "Cumulative" };

async function listClasses(): Promise<ShareClass[]> {
  const { data, error } = await api.GET("/v1/share-classes", { params: { query: { page_size: 200 } } });
  if (error) throw error;
  return data.items ?? [];
}

// preferenceText writes a class's liquidation preference.
function preferenceText(c: Draft): string {
  const l = c.liquidation;
  if (!l.multiple) return "None";
  return `${l.multiple}× ${l.participating ? `participating${l.cap_multiple ? `, capped at ${l.cap_multiple}×` : ""}` : "non-participating"}${l.seniority ? `, seniority ${l.seniority}` : ""}`;
}

function Rights({ c }: { c: ShareClass }) {
  return (
    <dl className="fy-dl fy-dl--3">
      <div>
        <dt>Votes per share</dt>
        <dd className="fy-num">{c.votes_per_share}</dd>
      </div>
      <div>
        <dt>Liquidation preference</dt>
        <dd>{preferenceText(c)}</dd>
      </div>
      <div>
        <dt>Dividends</dt>
        <dd>
          {dividends[c.dividend.kind]}
          {c.dividend.rate ? `, ${c.dividend.rate}% a year` : ""}
        </dd>
      </div>
      <div>
        <dt>Converts into ordinary</dt>
        <dd className="fy-num">{c.conversion_ratio ? `${c.conversion_ratio} : 1` : "Not given"}</dd>
      </div>
      <div>
        <dt>Anti-dilution</dt>
        <dd>{antiDilution[c.anti_dilution]}</dd>
      </div>
      <div>
        <dt>Pro-rata</dt>
        <dd>{c.pro_rata ? "Yes" : "No"}</dd>
      </div>
      {c.redemption && (
        <div className="fy-span-3">
          <dt>Redemption</dt>
          <dd>{c.redemption}</dd>
        </div>
      )}
    </dl>
  );
}

// draftOf is a class's rights as a draft takes them: its own fields only.
function draftOf(c: ShareClass): Draft {
  return {
    name: c.name,
    kind: c.kind,
    ...(c.series ? { series: c.series } : {}),
    account_id: c.account_id,
    votes_per_share: c.votes_per_share,
    liquidation: c.liquidation,
    dividend: c.dividend,
    ...(c.conversion_ratio ? { conversion_ratio: c.conversion_ratio } : {}),
    anti_dilution: c.anti_dilution,
    ...(c.pro_rata ? { pro_rata: true } : {}),
    ...(c.redemption ? { redemption: c.redemption } : {}),
    ...(c.note ? { note: c.note } : {}),
  };
}

const blank: Draft = {
  name: "",
  kind: "ordinary",
  account_id: "",
  votes_per_share: "1",
  liquidation: {},
  dividend: { kind: "none" },
  anti_dilution: "none",
  conversion_ratio: "",
  redemption: "",
  note: "",
};

// ClassForm drafts a class, or a new version of a recorded class's rights, or changes a draft (FR-EQ-16).
function ClassForm({ start, draft, onClose, onDone }: { start: Draft; draft: ShareClass | null; onClose: () => void; onDone: (text: string) => void }) {
  const id = useId();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const chart = useQuery({ queryKey: ["accounts", "all"], queryFn: listAccounts });
  const capital = (chart.data ?? []).filter((a) => a.role === "share-capital" && a.active && !a.group);
  const [form, setForm] = useState<Draft>(start);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const set = (k: keyof Draft, v: unknown) => setForm({ ...form, [k]: v });
  const account = form.account_id || capital[0]?.id || "";
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const body: Draft = { ...form, account_id: account };
    for (const k of ["series", "conversion_ratio", "redemption", "note", "class_id"] as const) if (!body[k]) delete body[k];
    const out = await guarded(confirm, () =>
      draft
        ? api.PUT("/v1/share-classes/{version_id}", {
            params: { path: { version_id: draft.version_id }, header: { "If-Match": `"${draft.version}-draft"`, "Idempotency-Key": key } },
            body,
          })
        : api.POST("/v1/share-classes", { params: { header: { "Idempotency-Key": key } }, body }),
    );
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    await queries.invalidateQueries({ queryKey: ["share-classes"] });
    onDone(draft ? "The draft is saved." : "The class is drafted. Attach the constitution, then ask for approval.");
    onClose();
  };
  const l = form.liquidation;
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} wide eyebrow="Share class"
      title={draft ? "Change the draft" : form.class_id ? "Draft new rights for the class" : "Draft a share class"}
      description="The rights are recorded as the constitution gives them; Fyniq does not compute them yet. Recorded once another person approves.">
      {problem && <Refusal problem={problem} title="The class was not saved" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-form fy-form--2">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-name`}>
              Name
            </label>
            <input className="fy-input" id={`${id}-name`} maxLength={100} value={form.name} onChange={(e) => set("name", e.target.value)} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-kind`}>
              Kind
            </label>
            <select className="fy-select" id={`${id}-kind`} value={form.kind} onChange={(e) => set("kind", e.target.value)}>
              <option value="ordinary">Ordinary</option>
              <option value="preference">Preference</option>
            </select>
          </div>
          {form.kind === "preference" && (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-series`}>
                Series
              </label>
              <input className="fy-input" id={`${id}-series`} maxLength={20} value={form.series ?? ""} onChange={(e) => set("series", e.target.value)} />
            </div>
          )}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-account`}>
              Share capital account
            </label>
            <select className="fy-select" id={`${id}-account`} value={account} disabled={Boolean(form.class_id)} onChange={(e) => set("account_id", e.target.value)}>
              {capital.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.code} {a.name}
                </option>
              ))}
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-votes`}>
              Votes per share
            </label>
            <input className="fy-input fy-num" id={`${id}-votes`} inputMode="decimal" value={form.votes_per_share} onChange={(e) => set("votes_per_share", e.target.value)} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-multiple`}>
              Liquidation preference (multiple)
            </label>
            <input className="fy-input fy-num" id={`${id}-multiple`} inputMode="decimal" placeholder="None" value={l.multiple ?? ""}
              onChange={(e) => set("liquidation", { ...l, multiple: e.target.value || undefined })} />
          </div>
          <label className="fy-row gap-2">
            <input type="checkbox" checked={Boolean(l.participating)} onChange={(e) => set("liquidation", { ...l, participating: e.target.checked })} />
            Participating
          </label>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-dividend`}>
              Dividends
            </label>
            <select className="fy-select" id={`${id}-dividend`} value={form.dividend.kind} onChange={(e) => set("dividend", { ...form.dividend, kind: e.target.value })}>
              {Object.entries(dividends).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-anti`}>
              Anti-dilution
            </label>
            <select className="fy-select" id={`${id}-anti`} value={form.anti_dilution} onChange={(e) => set("anti_dilution", e.target.value)}>
              {Object.entries(antiDilution).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </div>
          <label className="fy-row gap-2">
            <input type="checkbox" checked={Boolean(form.pro_rata)} onChange={(e) => set("pro_rata", e.target.checked)} />
            Pro-rata right in later rounds
          </label>
          <div className="fy-field fy-field--full">
            <label className="fy-label" htmlFor={`${id}-redeem`}>
              Redemption (optional)
            </label>
            <input className="fy-input" id={`${id}-redeem`} maxLength={500} value={form.redemption ?? ""} onChange={(e) => set("redemption", e.target.value)} />
          </div>
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy}>
            {draft ? "Save the draft" : "Draft the class"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// ShareClasses is Equity › Classes (docs/design/screens ShareClasses; FR-EQ-16, FR-EQ-27 Expert): every class with the
// rights in force, and each draft with its documents and approval.
export function ShareClasses() {
  const { session } = useSignedIn();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const q = useQuery({ queryKey: ["share-classes"], queryFn: listClasses });
  const [editing, setEditing] = useState<{ start: Draft; draft: ShareClass | null } | null>(null);
  const [status, setStatus] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const mayDraft = (session.roles ?? []).some((r) => drafters.includes(r));
  const draftH = helpText("share-class.draft");
  const askH = helpText("share-class.ask");
  const ask = async (c: ShareClass) => {
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/share-classes/{version_id}/approval", { params: { path: { version_id: c.version_id }, header: { "If-Match": `"${c.version}-draft"`, "Idempotency-Key": key } } }),
    );
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    setStatus(`Asked for approval of ${c.name}. Another Owner or the Controller approves it.`);
    await queries.invalidateQueries({ queryKey: ["share-classes"] });
  };
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Equity</span>
          <h1 className="fy-h1" title={helpText("equity.classes").tooltip}>
            Share classes and rights
          </h1>
          <p className="fy-secondary">Each class with its rights as the constitution gives them, recorded on another person's approval.</p>
        </div>
        {mayDraft && (
          <div className="fy-pagehead__actions">
            <Button variant="secondary" onClick={() => setEditing({ start: blank, draft: null })} title={draftH.tooltip}>
              <Icon of={Plus} />
              {draftH.label}
            </Button>
          </div>
        )}
      </div>
      <EquityTabs current="classes" />
      <p className="fy-small" role="status">
        {status}
      </p>
      {problem && <Refusal problem={problem} title="The approval was not asked for" />}
      {q.isError && <Refusal problem={problemOf(q.error)} title="The classes could not be shown" />}
      {q.data && q.data.length === 0 && (
        <p className="fy-small fy-muted">No share class is recorded yet. Until one is, shares count under their share capital account (3010 ordinary, 3020 preference).</p>
      )}
      {(q.data ?? []).map((c) => (
        <section key={c.version_id} className="fy-card" aria-labelledby={`class-${c.version_id}`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`class-${c.version_id}`}>
              {c.name}
              <span className="fy-small fy-muted"> · {c.kind === "preference" ? `preference${c.series ? ` series ${c.series}` : ""}` : "ordinary"} · {c.account_code}</span>
            </h2>
            {c.status === "recorded" ? (
              <span className="fy-row gap-2">
                <span className="fy-chip fy-chip--good">
                  <Icon of={CircleCheck} />
                  Recorded
                </span>
                {mayDraft && (
                  <Button size="sm" variant="ghost" onClick={() => setEditing({ start: { ...draftOf(c), class_id: c.id }, draft: null })}>
                    <Icon of={PencilLine} />
                    New rights
                  </Button>
                )}
              </span>
            ) : c.approval_id ? (
              <span className="fy-chip fy-chip--neutral">
                <Icon of={Hourglass} />
                <Link to="/approvals/$approvalId" params={{ approvalId: c.approval_id }}>
                  Waiting for approval
                </Link>
              </span>
            ) : (
              mayDraft && (
                <span className="fy-row gap-2">
                  <Button size="sm" variant="ghost" onClick={() => setEditing({ start: draftOf(c), draft: c })}>
                    <Icon of={PencilLine} />
                    Change
                  </Button>
                  <Button size="sm" onClick={() => void ask(c)} disabled={c.evidence === 0} title={askH.tooltip}>
                    <Icon of={Send} />
                    {askH.label}
                  </Button>
                </span>
              )
            )}
          </div>
          <div className="fy-card__body fy-stack fy-stack--3">
            <Rights c={c} />
            {c.status === "draft" && (
              <EvidencePanel record={{ kind: "share-class", id: c.version_id, etag: `"${c.version}-draft"` }} draft mayAttach={mayDraft && !c.approval_id}
                onChanged={(m) => {
                  setStatus(m);
                  void queries.invalidateQueries({ queryKey: ["share-classes"] });
                }} />
            )}
          </div>
        </section>
      ))}
      {editing && <ClassForm start={editing.start} draft={editing.draft} onClose={() => setEditing(null)} onDone={setStatus} />}
    </div>
  );
}
