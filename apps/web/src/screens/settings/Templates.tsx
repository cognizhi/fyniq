import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Download, Hourglass, LayoutTemplate, PencilLine, Plus, Upload } from "lucide-react";
import { useRef, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { SettingsPage } from "./SettingsPage";
import { docTypes, styles, templateApprovers, templatePreparers, typeNames, type DocType, type DocumentTemplate, type Kind, type StyleKey } from "./templateWords";

type Choice = components["schemas"]["DocumentTemplateChoice"];

async function allTemplates(): Promise<DocumentTemplate[]> {
  const out: DocumentTemplate[] = [];
  let cursor: string | undefined;
  do {
    const { data, error } = await api.GET("/v1/document-templates", { params: { query: { page_size: 200, cursor } } });
    if (error) throw error;
    out.push(...(data.items ?? []));
    cursor = data.next_cursor;
  } while (cursor && out.length < 1000);
  return out;
}

async function defaults(): Promise<Choice[]> {
  const { data, error } = await api.GET("/v1/document-template-defaults", { params: { query: { page_size: 16 } } });
  if (error) throw error;
  return data.items ?? [];
}

async function waitingPublications() {
  const { data, error } = await api.GET("/v1/approvals", { params: { query: { status: "pending", page_size: 200 } } });
  if (error) throw error;
  return (data.items ?? []).filter((a) => a.object_type === "document_template");
}

// StyleThumb is a starter style's thumbnail: the shape of its page, drawn with tokens (decorative).
export function StyleThumb({ style }: { style: StyleKey }) {
  const bar = (w: string, c: string, extra = "") => <span className={`block h-1 rounded-sm ${extra}`} style={{ width: w, background: `var(${c})` }} />;
  return (
    <span aria-hidden="true" className="flex aspect-[210/297] w-full max-w-32 flex-col gap-1.5 overflow-hidden rounded-md border border-[var(--border)] bg-[var(--surface-1)]" style={{ padding: style === "band" ? 0 : style === "compact" ? 6 : style === "minimal" ? 14 : 10 }}>
      {style === "band" ? <span className="block h-[16%] bg-[var(--text-primary)]" /> : bar("50%", style === "minimal" ? "--text-secondary" : "--primary", "self-end h-1.5")}
      <span className={style === "band" ? "mx-2.5 flex flex-col gap-1.5" : "flex flex-col gap-1.5"}>
        {bar("45%", "--border-strong", "mt-1")}
        {style !== "minimal" && <span className="mt-1.5 block h-0.5" style={{ background: style === "band" ? "var(--text-primary)" : "var(--primary)" }} />}
        {bar("100%", "--surface-3")}
        {bar("100%", "--surface-3")}
        {style === "compact" && bar("100%", "--surface-3")}
        {bar("40%", "--text-secondary", "self-end mt-1 h-1.5")}
      </span>
    </span>
  );
}

// NewTemplate makes a template from a starter (FR-TPL-07) as a draft, and opens it.
function NewTemplate({ start, onClose }: { start: { kind: Kind; type: DocType; style: StyleKey } | null; onClose: () => void }) {
  const navigate = useNavigate();
  const [kind, setKind] = useState<Kind>(start?.kind ?? "document");
  const [type, setType] = useState<DocType>(start?.type ?? "invoice");
  const [style, setStyle] = useState<StyleKey>(start?.style ?? "classic");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const add = helpText("template.new");
  const create = async () => {
    setBusy(true);
    setProblem(null);
    const body = { kind, document_type: type, name: name.trim() || `${typeNames[type]} ${kind === "email" ? "email" : styles.find((s) => s.key === style)?.name.toLowerCase()}`, ...(kind === "document" ? { starter: style } : {}) };
    const out = await api.POST("/v1/document-templates", { params: { header: { "Idempotency-Key": newKey() } }, body });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onClose();
    await navigate({ to: "/settings/templates/$templateId", params: { templateId: out.data.id } });
  };
  return (
    <Dialog
      open={start !== null}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      eyebrow="Templates"
      title={add.label}
      description="A new template is a draft: nothing changes on your documents until it is published."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void create()} disabled={busy} title={add.tooltip}>
            Make the draft
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The template was not made" />}
      <div className="fy-form fy-form--2">
        <div className="fy-field">
          <label className="fy-label" htmlFor="new-type">
            Document
          </label>
          <select className="fy-select" id="new-type" value={type} onChange={(e) => setType(e.target.value as DocType)}>
            {docTypes.map((t) => (
              <option key={t} value={t}>
                {typeNames[t]}
              </option>
            ))}
          </select>
        </div>
        <div className="fy-field">
          <span className="fy-label" id="new-kind">
            Kind
          </span>
          <div className="fy-segmented" role="radiogroup" aria-labelledby="new-kind">
            {(["document", "email"] as const).map((k) => (
              <button key={k} type="button" role="radio" aria-checked={kind === k} onClick={() => setKind(k)}>
                {k === "document" ? "The document" : "Its email"}
              </button>
            ))}
          </div>
        </div>
        <div className="fy-field fy-field--full">
          <label className="fy-label" htmlFor="new-name">
            Name
          </label>
          <input className="fy-input" id="new-name" value={name} maxLength={100} placeholder={`${typeNames[type]} ${kind === "email" ? "email" : "template"}`} onChange={(e) => setName(e.target.value)} />
        </div>
      </div>
      {kind === "document" && (
        <fieldset className="fy-fieldset">
          <legend className="fy-label">Style</legend>
          <div className="fy-row fy-row--wrap fy-row--top gap-2" role="radiogroup" aria-label="Style">
            {styles.map((s) => (
              <button key={s.key} type="button" role="radio" aria-checked={style === s.key} className="fy-choice min-w-0 flex-[1_1_80px] flex-col items-stretch gap-1.5 p-2" onClick={() => setStyle(s.key)}>
                <StyleThumb style={s.key} />
                <span className="fy-strong">{s.name}</span>
              </button>
            ))}
          </div>
        </fieldset>
      )}
    </Dialog>
  );
}

// ChooseDefault lets an approver choose a kind and type's default among its published templates, or the platform's starter
// (FR-TPL-05; the owner's decision of 23 Oct 2026: no separate request).
function ChooseDefault({ choice, templates, onClose }: { choice: Choice | null; templates: DocumentTemplate[]; onClose: () => void }) {
  const queries = useQueryClient();
  const [pick, setPick] = useState<string>(choice?.template?.id ?? "");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const published = templates.filter((t) => choice && t.kind === choice.kind && t.document_type === choice.document_type && t.published_version);
  const save = async () => {
    if (!choice) return;
    setBusy(true);
    setProblem(null);
    const out = await api.PUT("/v1/document-template-defaults/{kind}/{document_type}", {
      params: { path: { kind: choice.kind, document_type: choice.document_type }, header: { "If-Match": choice.etag, "Idempotency-Key": newKey() } },
      body: pick ? { template_id: pick } : {},
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["document-template-defaults"] });
    onClose();
  };
  const what = choice ? `${typeNames[choice.document_type]}${choice.kind === "email" ? " email" : ""}` : "";
  return (
    <Dialog
      open={choice !== null}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      eyebrow="Templates"
      title={`Default for ${what.toLowerCase()}s`}
      description="Every published template was approved when it was published, so choosing among them needs no second approval. A customer's own template comes first."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void save()} disabled={busy}>
            Use this default
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The default was not changed" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor="default-pick">
          Template
        </label>
        <select className="fy-select" id="default-pick" value={pick} onChange={(e) => setPick(e.target.value)}>
          <option value="">The Classic starter (version 0)</option>
          {published.map((t) => (
            <option key={t.id} value={t.id}>
              {t.name} (version {t.published_version})
            </option>
          ))}
        </select>
        {published.length === 0 && <span className="fy-help">No {what.toLowerCase()} template is published yet.</span>}
      </div>
    </Dialog>
  );
}

function DefaultName({ choice }: { choice?: Choice }) {
  if (!choice?.template)
    return (
      <span className="fy-muted">
        Classic starter<span className="fy-small block">Version 0</span>
      </span>
    );
  return (
    <span>
      {choice.template.name}
      <span className="fy-small fy-muted block">Version {choice.template.published_version}</span>
    </span>
  );
}

// Templates is Settings › Templates (docs/design/screens DocumentTemplates; FR-TPL-04, FR-TPL-05, FR-TPL-07): each document
// type's default document and email template, every template with its version in force and draft, the starter gallery,
// and bundles to move templates between organisations.
export function Templates() {
  const { session, mode } = useSignedIn();
  const queries = useQueryClient();
  const roles = session.roles ?? [];
  const mayPrepare = roles.some((r) => templatePreparers.includes(r));
  const mayApprove = roles.some((r) => templateApprovers.includes(r));
  const list = useQuery({ queryKey: ["document-templates"], queryFn: allTemplates });
  const chosen = useQuery({ queryKey: ["document-template-defaults"], queryFn: defaults });
  const waiting = useQuery({ queryKey: ["approvals", "document_template"], queryFn: waitingPublications });
  const [start, setStart] = useState<{ kind: Kind; type: DocType; style: StyleKey } | null>(null);
  const [choosing, setChoosing] = useState<Choice | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [status, setStatus] = useState("");
  const file = useRef<HTMLInputElement>(null);
  const templates = list.data ?? [];
  const add = helpText("template.new");

  const exportAll = async () => {
    setProblem(null);
    const ids = templates.slice(0, 50).map((t) => t.id);
    if (ids.length === 0) return;
    const out = await api.GET("/v1/document-template-bundle", { params: { query: { template_id: ids } } });
    if (!out.data) return setProblem(problemOf(out.error));
    const blob = new Blob([JSON.stringify(out.data, null, 2)], { type: "application/json" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `templates-${out.data.exported_at.slice(0, 10)}.json`;
    a.click();
    URL.revokeObjectURL(a.href);
    setStatus(`Exported ${ids.length} template${ids.length === 1 ? "" : "s"}.`);
  };
  const importBundle = async (f: File) => {
    setProblem(null);
    let bundle: components["schemas"]["TemplateBundle"];
    try {
      bundle = JSON.parse(await f.text());
    } catch {
      return setProblem({ detail: `${f.name} is not a bundle of templates.`, fix: "Choose a file you exported from Fyniq." });
    }
    const out = await api.POST("/v1/document-template-bundles", { params: { header: { "Idempotency-Key": newKey() } }, body: bundle });
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["document-templates"] });
    const n = out.data.templates.length;
    setStatus(`Imported ${n} template${n === 1 ? "" : "s"} as drafts: each is published like any other.`);
  };

  const choiceOf = (kind: Kind, type: DocType) => chosen.data?.find((c) => c.kind === kind && c.document_type === type);
  const pending = waiting.data ?? [];

  return (
    <SettingsPage
      screen="document-templates"
      title="Templates"
      lead="How every document and email you send looks. Issued documents keep the version they were issued with; a customer's own template comes before the default."
      actions={
        mayPrepare && (
          <Button variant="primary" onClick={() => setStart({ kind: "document", type: "invoice", style: "classic" })} title={add.tooltip}>
            <Icon of={Plus} />
            {add.label}
          </Button>
        )
      }
    >
      {pending.length > 0 && (
        <div className="fy-banner fy-banner--info" role="status">
          <Icon of={Hourglass} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">{pending.length === 1 ? "1 publication is waiting for approval" : `${pending.length} publications are waiting for approval`}</span>
            <span className="fy-small">
              {pending[0]?.summary}. Documents keep using the version in force until it is approved. <Link to="/approvals">Review it</Link>
            </span>
          </div>
        </div>
      )}
      {problem && <Refusal problem={problem} title="That did not work" />}
      {status && (
        <p className="fy-small" role="status">
          {status}
        </p>
      )}
      {list.isError && <Refusal problem={problemOf(list.error)} title="The templates could not be listed" />}

      {(["document", "email"] as const).map((kind) => (
        <section key={kind} className="fy-card" aria-labelledby={`defaults-${kind}`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`defaults-${kind}`}>
              {kind === "document" ? "Document templates" : "Email templates"}
            </h2>
            <span className="fy-small fy-muted">{kind === "document" ? "Rendered as archival, accessible PDFs" : "The subject and body use the same fields"}</span>
          </div>
          <div className="fy-tablewrap">
            <table className="fy-table fy-table--cards">
              <thead>
                <tr>
                  <th scope="col">Document</th>
                  <th scope="col">Default</th>
                  <th scope="col">Templates</th>
                  {mayApprove && (
                    <th scope="col">
                      <span className="fy-sr">Actions</span>
                    </th>
                  )}
                </tr>
              </thead>
              <tbody className="fy-stagger">
                {docTypes.map((type) => {
                  const mine = templates.filter((t) => t.kind === kind && t.document_type === type);
                  const c = choiceOf(kind, type);
                  return (
                    <tr key={type}>
                      <td data-card="title">
                        {typeNames[type]}
                        {kind === "document" && (type === "invoice" || type === "credit_note") && <span className="fy-small fy-muted block">IRAS particulars locked</span>}
                      </td>
                      <td data-card="meta">
                        <DefaultName choice={c} />
                      </td>
                      <td data-card="meta">
                        {mine.length === 0 ? (
                          <span className="fy-muted">None yet</span>
                        ) : (
                          <span className="fy-row fy-row--wrap gap-2">
                            {mine.map((t) => (
                              <Link key={t.id} to="/settings/templates/$templateId" params={{ templateId: t.id }} className="fy-row gap-1">
                                {t.name}
                                {t.draft_changed && (
                                  <span className="fy-chip fy-chip--neutral">
                                    <Icon of={PencilLine} />
                                    {t.published_version ? "Draft changes" : "Draft"}
                                  </span>
                                )}
                              </Link>
                            ))}
                          </span>
                        )}
                      </td>
                      {mayApprove && (
                        <td data-card="status">
                          <Button size="sm" variant="ghost" onClick={() => c && setChoosing(c)} disabled={!c}>
                            Change default <span className="fy-sr">for {typeNames[type].toLowerCase()} {kind}s</span>
                          </Button>
                        </td>
                      )}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </section>
      ))}

      <section className="fy-card" aria-labelledby="gallery-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="gallery-title">
            Starter gallery
          </h2>
          <span className="fy-small fy-muted">Four styles; your logo, details and fonts applied</span>
        </div>
        <div className="fy-card__body">
          <div className="fy-grid fy-grid--4 gap-3">
            {styles.map((s) => (
              <div key={s.key} className="fy-stack fy-stack--2">
                <StyleThumb style={s.key} />
                <div className="fy-row gap-1.5">
                  <span className="fy-stack fy-grow gap-0">
                    <span className="fy-strong">{s.name}</span>
                    <span className="fy-small fy-muted">{s.description}</span>
                  </span>
                  {mayPrepare && (
                    <Button size="sm" variant="ghost" onClick={() => setStart({ kind: "document", type: "invoice", style: s.key })}>
                      Use <span className="fy-sr">the {s.name} style</span>
                    </Button>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
        <div className="fy-card__foot">
          <span className="fy-small fy-muted">Starting from a style makes a draft; nothing changes until it is published. Until a type has a template, the Classic starter renders its documents.</span>
        </div>
      </section>

      {mode === "expert" && (
        <section className="fy-card" aria-labelledby="bundle-title">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="bundle-title">
              Template bundle
            </h2>
          </div>
          <div className="fy-card__body fy-row fy-row--wrap fy-row--top gap-4">
            <div className="fy-stack fy-stack--2 flex-[1_1_260px]">
              <span className="fy-label">Export or import templates as one versioned bundle</span>
              <span className="fy-help">A JSON file holding each template's version in force, or its draft if it was never published, and its fingerprint. An import arrives as drafts and is refused if a template changed after export.</span>
            </div>
            <div className="fy-row fy-row--wrap gap-2">
              <Button size="sm" onClick={() => void exportAll()} disabled={templates.length === 0}>
                <Icon of={Download} />
                Export bundle
              </Button>
              {mayPrepare && (
                <>
                  <Button size="sm" variant="ghost" onClick={() => file.current?.click()}>
                    <Icon of={Upload} />
                    Import bundle
                  </Button>
                  <input
                    ref={file}
                    type="file"
                    accept="application/json,.json"
                    className="fy-sr"
                    aria-label="Bundle to import"
                    tabIndex={-1}
                    onChange={(e) => {
                      const f = e.target.files?.[0];
                      if (f) void importBundle(f);
                      e.target.value = "";
                    }}
                  />
                </>
              )}
            </div>
          </div>
        </section>
      )}

      {list.data && templates.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={LayoutTemplate} size={24} />
          <h2 className="fy-h3">No templates yet</h2>
          <p className="fy-secondary">Your documents use the Classic starter until you publish a template of your own. Start from a style above.</p>
        </div>
      )}
      <NewTemplate key={start ? `new-${start.kind}-${start.type}-${start.style}` : "new"} start={start} onClose={() => setStart(null)} />
      <ChooseDefault key={choosing ? `choose-${choosing.kind}-${choosing.document_type}` : "choose"} choice={choosing} templates={templates} onClose={() => setChoosing(null)} />
    </SettingsPage>
  );
}
