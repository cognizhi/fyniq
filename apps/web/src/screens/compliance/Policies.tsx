import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BookOpenCheck, CircleCheck, ClockAlert, FilePen, Send } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Policy = components["schemas"]["PolicyView"];
type Key = Policy["key"];

async function policies() {
  const { data, error } = await api.GET("/v1/compliance/policies");
  if (error) throw error;
  return data.items;
}

async function policy(key: Key) {
  const { data, error } = await api.GET("/v1/compliance/policies/{key}", { params: { path: { key } } });
  if (error) throw error;
  return data;
}

// The roles that draft and publish the library (Appendix D "Policy library", C/A).
const editors = ["owner", "dpo"];

function MineChip({ p }: { p: Policy }) {
  if (!p.mine) return null;
  if (p.mine.acknowledged_at)
    return (
      <span className="fy-chip fy-chip--good">
        <Icon of={CircleCheck} />
        Acknowledged
      </span>
    );
  return (
    <span className={`fy-chip ${p.mine.overdue ? "fy-chip--serious" : "fy-chip--warning"}`}>
      <Icon of={ClockAlert} />
      {p.mine.overdue ? "Overdue" : "Due"} {date(p.mine.due_on)}
    </span>
  );
}

// PolicyLibrary is the policy library of Compliance › Risks and policies (CMP-07): each policy's version in force, how
// many members acknowledged it, the reader's own acknowledgement, and for the Owner and the Compliance officer the draft
// to edit and publish.
export function PolicyLibrary() {
  const id = useId();
  const { session } = useSignedIn();
  const editor = (session.roles ?? []).some((r) => editors.includes(r));
  const list = useQuery({ queryKey: ["policies"], queryFn: policies });
  const [reading, setReading] = useState<Key | null>(null);
  const [editing, setEditing] = useState<Key | null>(null);
  return (
    <section className="fy-card" aria-labelledby={`${id}-policies`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-policies`}>
          Policy library
        </h2>
        <span className="fy-small fy-muted">Each member acknowledges each version within 30 days</span>
      </div>
      {list.isError && <Refusal problem={problemOf(list.error)} title="The policy library could not be shown" />}
      {list.data && (
        <ul className="fy-list" aria-labelledby={`${id}-policies`}>
          {list.data.map((p) => (
            <li key={p.key} className="fy-list__item">
              <span className="fy-list__main">
                <span className="fy-list__title">{p.title}</span>
                <span className="fy-list__meta whitespace-normal">
                  {p.current
                    ? `Version ${p.current.version}, published ${date(p.current.published_at)} · ${p.current.acknowledged} of ${p.members} acknowledged`
                    : "Not published yet"}
                  {editor && p.draft.unpublished && (p.draft.source === "fyniq" ? " · Fyniq's draft ready to adopt" : ` · draft revision ${p.draft.revision} not published`)}
                </span>
              </span>
              <MineChip p={p} />
              <span className="fy-row gap-2">
                {p.current && (
                  <Button size="sm" variant="ghost" onClick={() => setReading(p.key)} aria-label={`Read ${p.title}`}>
                    <Icon of={BookOpenCheck} />
                    Read
                  </Button>
                )}
                {editor && (
                  <Button size="sm" variant="secondary" onClick={() => setEditing(p.key)} aria-label={`Draft ${p.title}`} title={helpText("policy.publish").tooltip}>
                    <Icon of={FilePen} />
                    Draft
                  </Button>
                )}
              </span>
            </li>
          ))}
        </ul>
      )}
      {reading && <PolicyReader policyKey={reading} onClose={() => setReading(null)} />}
      {editing && <PolicyEditor policyKey={editing} onClose={() => setEditing(null)} />}
    </section>
  );
}

// PolicyReader shows a policy's version in force, and lets the reader acknowledge it for themselves (CMP-07).
export function PolicyReader({ policyKey, onClose }: { policyKey: Key; onClose: () => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const one = useQuery({ queryKey: ["policy", policyKey], queryFn: () => policy(policyKey) });
  const p = one.data;
  const ack = useMutation({
    mutationFn: async () => {
      const version = p!.current!.version;
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/policies/{key}/versions/{version}/acknowledgements", {
          params: { path: { key: policyKey, version }, header: { "Idempotency-Key": newKey(), "If-Match": `"v${version}"` } },
        }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await queries.invalidateQueries({ queryKey: ["policies"] });
      await queries.invalidateQueries({ queryKey: ["policy", policyKey] });
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  return (
    <Dialog open onOpenChange={(o) => !o && !ack.isPending && onClose()} dismissible={!ack.isPending} eyebrow="Policy library" title={p?.title ?? "Policy"}
      description={p?.current ? `Version ${p.current.version}, published ${date(p.current.published_at)}.` : undefined}>
      {problem && <Refusal problem={problem} title="The acknowledgement was not recorded" />}
      {one.isError && <Refusal problem={problemOf(one.error)} title="The policy could not be shown" />}
      {p && (
        <div className="fy-stack fy-stack--3">
          <div className="fy-small whitespace-pre-wrap" role="document" aria-label={`${p.title}, version ${p.current?.version ?? ""}`} style={{ maxHeight: "50vh", overflow: "auto" }}>
            {p.body}
          </div>
          {p.mine?.acknowledged_at ? (
            <p className="fy-small fy-secondary">You acknowledged this version on {dateTime(p.mine.acknowledged_at)}.</p>
          ) : (
            p.current && (
              <div className="fy-row justify-end gap-2">
                <Button variant="ghost" type="button" onClick={onClose} disabled={ack.isPending}>
                  Later
                </Button>
                <Button variant="primary" onClick={() => ack.mutate()} disabled={ack.isPending} title={helpText("policy.acknowledge").tooltip}>
                  <Icon of={CircleCheck} />I have read and accept it
                </Button>
              </div>
            )
          )}
        </div>
      )}
    </Dialog>
  );
}

// PolicyEditor edits a policy's draft and publishes it as the next version (CMP-07): the Owner and the Compliance officer.
function PolicyEditor({ policyKey, onClose }: { policyKey: Key; onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const one = useQuery({ queryKey: ["policy", policyKey], queryFn: () => policy(policyKey) });
  const [body, setBody] = useState<string | null>(null);
  const p = one.data;
  const text = body ?? p?.draft.body ?? "";
  const done = async () => {
    await queries.invalidateQueries({ queryKey: ["policies"] });
    await queries.invalidateQueries({ queryKey: ["policy", policyKey] });
  };
  const save = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () =>
        api.PUT("/v1/compliance/policies/{key}/draft", {
          params: { path: { key: policyKey }, header: { "Idempotency-Key": newKey(), "If-Match": `"r${p!.draft.revision}"` } },
          body: { body: text },
        }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      setBody(null);
      await done();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const publish = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/policies/{key}/versions", {
          params: { path: { key: policyKey }, header: { "Idempotency-Key": newKey(), "If-Match": `"r${p!.draft.revision}"` } },
        }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await done();
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const busy = save.isPending || publish.isPending;
  const next = (p?.current?.version ?? 0) + 1;
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} eyebrow="Policy library" title={p ? `Draft: ${p.title}` : "Draft"}
      description={p?.draft.source === "fyniq" ? "Fyniq's starting draft, written from official sources. Adopt it as it is, or edit it first." : `Revision ${p?.draft.revision ?? ""} of your draft.`}>
      {problem && <Refusal problem={problem} title="The draft was not saved or published" />}
      {p && (
        <div className="fy-stack fy-stack--3">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-body`}>
              Text (Markdown)
            </label>
            <textarea className="fy-textarea" id={`${id}-body`} rows={16} value={text} onChange={(e) => setBody(e.target.value)} />
            <span className="fy-help">Clauses marked [counsel to confirm] bear on the law: have counsel confirm them before you publish.</span>
          </div>
          <div className="fy-row justify-end gap-2">
            <Button variant="secondary" onClick={() => save.mutate()} disabled={busy || body === null || body === p.draft.body}>
              <Icon of={FilePen} />
              Save the draft
            </Button>
            <Button variant="primary" onClick={() => publish.mutate()} disabled={busy || body !== null || !p.draft.unpublished}>
              <Icon of={Send} />
              Publish version {next}
            </Button>
          </div>
        </div>
      )}
    </Dialog>
  );
}

// PoliciesToAcknowledge is the "Acknowledge policy" card of the Inbox and Home (CMP-07): the versions in force the reader
// has not acknowledged, with when each falls due; nothing when none waits.
export function PoliciesToAcknowledge() {
  const id = useId();
  const list = useQuery({ queryKey: ["policies"], queryFn: policies });
  const [reading, setReading] = useState<Key | null>(null);
  const waiting = (list.data ?? []).filter((p) => p.mine && !p.mine.acknowledged_at);
  if (waiting.length === 0) return null;
  return (
    <section className="fy-card" aria-labelledby={`${id}-ack`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-ack`} title={helpText("policy.acknowledge").tooltip}>
          Acknowledge policy
        </h2>
      </div>
      <ul className="fy-list" aria-labelledby={`${id}-ack`}>
        {waiting.map((p) => (
          <li key={p.key} className="fy-list__item">
            <span className="fy-list__main">
              <span className="fy-list__title">{p.title}</span>
              <span className="fy-list__meta">Version {p.mine!.version}</span>
            </span>
            <MineChip p={p} />
            <Button size="sm" variant="secondary" onClick={() => setReading(p.key)} aria-label={`Read and acknowledge ${p.title}`}>
              <Icon of={BookOpenCheck} />
              Read
            </Button>
          </li>
        ))}
      </ul>
      {reading && <PolicyReader policyKey={reading} onClose={() => setReading(null)} />}
    </section>
  );
}
