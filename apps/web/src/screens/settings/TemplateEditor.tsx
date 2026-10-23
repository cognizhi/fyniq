import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { Braces, CircleCheck, CircleX, Download, FileClock, Lock, PencilLine, Send, Undo2, UserCheck } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { StyleThumb } from "./Templates";
import { blockName, contrastOnWhite, styles, templateApprovers, templatePreparers, typeNames, type DocumentTemplate, type StyleKey, type TemplateBlock, type TemplateCatalogue, type TemplateContent } from "./templateWords";

type Version = components["schemas"]["TemplateVersion"];
type Preview = components["schemas"]["TemplatePreview"];
type Texts = NonNullable<TemplateBlock["texts"]>;

// The code the API answers when another person approves a publication (Standard mode): ask for approval.
const needsApproval = "TPL-14";

const clone = <T,>(x: T): T => JSON.parse(JSON.stringify(x)) as T;

// fieldErrors are the API's problems by the path of the field they name, for showing each under its field.
function fieldErrors(p: ApiProblem | null): Map<string, string> {
  const out = new Map<string, string>();
  for (const e of p?.errors ?? []) if (e.field && e.message) out.set(e.field.replace(/^body\./, ""), `${e.message}. ${e.fix ?? ""}`.trim());
  return out;
}

// FieldPicker inserts a field of the catalogue, in a format its type takes, into a text (FR-TPL-02): texts hold words and
// fields only, written {{path}} or {{path|format}}.
function FieldPicker({ catalogue, onInsert, label }: { catalogue: TemplateCatalogue; onInsert: (token: string) => void; label: string }) {
  const [path, setPath] = useState("");
  const [format, setFormat] = useState("");
  const field = catalogue.fields.find((f) => f.path === path);
  const formats = catalogue.formats.find((g) => g.type === field?.type)?.formats ?? [];
  return (
    <div className="fy-row fy-row--wrap gap-2" role="group" aria-label={`Insert a field into ${label}`}>
      <select className="fy-select w-auto" aria-label="Field" value={path} onChange={(e) => (setPath(e.target.value), setFormat(""))}>
        <option value="">Choose a field…</option>
        {catalogue.fields.map((f) => (
          <option key={f.path} value={f.path}>
            {f.label}
          </option>
        ))}
      </select>
      {formats.length > 0 && (
        <select className="fy-select w-auto" aria-label="Format" value={format} onChange={(e) => setFormat(e.target.value)}>
          {formats.map((f, i) => (
            <option key={f} value={i === 0 ? "" : f}>
              {f}
              {i === 0 ? " (the default)" : ""}
            </option>
          ))}
        </select>
      )}
      <Button size="sm" disabled={!path} onClick={() => (onInsert(`{{${path}${format ? `|${format}` : ""}}}`), setPath(""), setFormat(""))}>
        <Icon of={Braces} />
        Insert field
      </Button>
    </div>
  );
}

// TextField is one of a block's texts, or an email's subject or body, with the field picker for a body or subject.
function TextField({ id, label, value, multiline, max, catalogue, error, onChange }: { id: string; label: string; value: string; multiline?: boolean; max: number; catalogue?: TemplateCatalogue; error?: string; onChange: (v: string) => void }) {
  const ref = useRef<HTMLTextAreaElement & HTMLInputElement>(null);
  const insert = (token: string) => {
    const el = ref.current;
    const at = el?.selectionStart ?? value.length;
    onChange(value.slice(0, at) + token + value.slice(el?.selectionEnd ?? at));
  };
  const errId = `${id}-error`;
  return (
    <div className="fy-field fy-field--full">
      <label className="fy-label" htmlFor={id}>
        {label}
      </label>
      {multiline ? (
        <textarea ref={ref} className="fy-textarea" id={id} value={value} maxLength={max} rows={4} aria-invalid={error ? true : undefined} aria-describedby={error ? errId : undefined} onChange={(e) => onChange(e.target.value)} />
      ) : (
        <input ref={ref} className="fy-input" id={id} value={value} maxLength={max} aria-invalid={error ? true : undefined} aria-describedby={error ? errId : undefined} onChange={(e) => onChange(e.target.value)} />
      )}
      {error && (
        <span className="fy-error" id={errId}>
          <Icon of={CircleX} />
          {error}
        </span>
      )}
      {catalogue && <FieldPicker catalogue={catalogue} label={label} onInsert={insert} />}
    </div>
  );
}

// TemplateEditor is Settings › Templates › a template (docs/design/screens TemplateEditor; FR-TPL-01 to FR-TPL-04, the P1
// half): its style, theme and page, its blocks shown or hidden with their texts, the typed fields, a live preview of the
// organisation's details and sample data, publication (in Founder mode by an approver alone, otherwise by approval), and
// its versions with roll back.
export function TemplateEditor() {
  const { templateId } = useParams({ strict: false }) as { templateId: string };
  const { session, mode } = useSignedIn();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const roles = session.roles ?? [];
  const mayPrepare = roles.some((r) => templatePreparers.includes(r));
  const mayApprove = roles.some((r) => templateApprovers.includes(r));
  const h = (k: string) => helpText(k);

  const one = useQuery({
    queryKey: ["document-template", templateId],
    queryFn: async () => {
      const r = await api.GET("/v1/document-templates/{template_id}", { params: { path: { template_id: templateId } } });
      if (r.error) throw r.error;
      return { template: r.data, etag: etagOf(r.response) || `"${r.data.version}"` };
    },
  });
  const t: DocumentTemplate | undefined = one.data?.template;
  const catalogue = useQuery({
    queryKey: ["document-template-catalogue", t?.document_type],
    enabled: !!t,
    queryFn: async () => {
      const r = await api.GET("/v1/document-template-catalogue/{document_type}", { params: { path: { document_type: t?.document_type ?? "invoice" } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const versions = useQuery({
    queryKey: ["document-template-versions", templateId],
    queryFn: async () => {
      const r = await api.GET("/v1/document-templates/{template_id}/versions", { params: { path: { template_id: templateId }, query: { page_size: 50 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });

  const [name, setName] = useState("");
  const [content, setContent] = useState<TemplateContent | null>(null);
  const [open, setOpen] = useState<number | null>(null);
  // The fields are a reference: open beside the preview on wide screens, folded on narrow ones so the preview follows the blocks.
  const [fieldsShown, setFieldsShown] = useState(() => window.matchMedia("(min-width: 1024px)").matches);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [failed, setFailed] = useState("");
  const [status, setStatus] = useState("");
  const [note, setNote] = useState("");
  const [rollback, setRollback] = useState<Version | null>(null);
  useEffect(() => {
    if (t) {
      setName(t.name);
      setContent(clone(t.draft));
    }
  }, [t]);
  const dirty = !!t && !!content && (name !== t.name || JSON.stringify(content) !== JSON.stringify(t.draft));

  // The live preview: the content as it is on the screen, rendered after a pause in typing.
  const [preview, setPreview] = useState<{ result?: Preview; problem?: ApiProblem; busy: boolean }>({ busy: false });
  useEffect(() => {
    if (!t || !content) return;
    let stale = false;
    setPreview((p) => ({ ...p, busy: true }));
    const timer = setTimeout(async () => {
      const r = await api.POST("/v1/document-template-previews", { body: { kind: t.kind, document_type: t.document_type, content } });
      if (stale) return;
      setPreview(r.data ? { result: r.data, busy: false } : { problem: problemOf(r.error), busy: false });
    }, 600);
    return () => {
      stale = true;
      clearTimeout(timer);
    };
  }, [t, content]);
  const pdfLink = useMemo(() => {
    const pdf = preview.result?.pdf;
    if (!pdf) return null;
    const bytes = Uint8Array.from(atob(pdf), (c) => c.charCodeAt(0));
    return URL.createObjectURL(new Blob([bytes], { type: "application/pdf" }));
  }, [preview.result]);
  useEffect(() => () => void (pdfLink && URL.revokeObjectURL(pdfLink)), [pdfLink]);

  const refresh = () => Promise.all(["document-template", "document-templates", "document-template-versions", "approvals"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
  const save = async () => {
    if (!one.data || !content) return;
    setBusy(true);
    setProblem(null);
    setStatus("");
    setFailed("The draft was not saved");
    const r = await api.PUT("/v1/document-templates/{template_id}", { params: { path: { template_id: templateId }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } }, body: { name, content } });
    setBusy(false);
    if (!r.data) return setProblem(problemOf(r.error));
    await refresh();
    setStatus("Saved. Documents keep using the version in force until this draft is published.");
  };
  const publish = async (ask: boolean, version?: number) => {
    if (!one.data) return;
    setBusy(true);
    setProblem(null);
    setStatus("");
    setFailed(ask ? "The request was not made" : "The template was not published");
    const params = { path: { template_id: templateId }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } };
    const body = { ...(note.trim() ? { note: note.trim() } : {}), ...(version ? { version } : {}) };
    const r = ask
      ? await guarded(confirm, () => api.POST("/v1/document-templates/{template_id}/approval", { params, body }))
      : await api.POST("/v1/document-templates/{template_id}/publish", { params, body });
    setBusy(false);
    setRollback(null);
    if (!r.data) return setProblem(problemOf(r.error));
    setNote("");
    await refresh();
    setStatus(ask ? "Asked for approval: the version is published when another person approves it." : "Published. New documents render with this version.");
  };

  if (one.isPending) return <p className="fy-secondary">Loading the template…</p>;
  if (one.isError || !t) return <Refusal problem={problemOf(one.error)} title="The template could not be read" />;
  if (!content || !catalogue.data) return <p className="fy-secondary">Loading the template…</p>;
  const cat = catalogue.data;
  const errs = fieldErrors(problem);
  const set = (f: (c: TemplateContent) => void) =>
    setContent((c) => {
      if (!c) return c;
      const n = clone(c);
      f(n);
      return n;
    });
  const setText = (i: number, key: keyof Texts, v: string) =>
    set((c) => {
      const b = c.blocks?.[i];
      if (b) b.texts = { ...(b.texts ?? {}), [key]: v };
    });
  const kindOf = (k: string) => cat.blocks.find((b) => b.kind === k);
  const ratio = content.theme ? contrastOnWhite(content.theme.accent) : null;
  const askOnly = problem?.code === needsApproval;
  const what = `${typeNames[t.document_type]}${t.kind === "email" ? " email" : ""}`;
  const latest = versions.data?.[0];

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/settings">Settings</Link>
            <span aria-hidden="true">›</span>
            <Link to="/settings/templates">Templates</Link>
          </nav>
          <h1 className="fy-h1">
            {what} template: {t.name}
          </h1>
          <div className="fy-row fy-row--wrap gap-2">
            {(t.draft_changed || dirty) && (
              <span className="fy-chip fy-chip--neutral">
                <Icon of={PencilLine} />
                {dirty ? "Unsaved changes" : "Draft changes"}
              </span>
            )}
            {t.published_version ? (
              <span className="fy-chip fy-chip--good">
                <Icon of={CircleCheck} />
                Version {t.published_version} in force
              </span>
            ) : (
              <span className="fy-chip fy-chip--outline">Never published</span>
            )}
            <span className="fy-small fy-muted">
              Saved{t.saved_by?.name ? ` by ${t.saved_by.name}` : ""}, {date(t.saved_at)}
            </span>
          </div>
        </div>
        {mayPrepare && (
          <div className="fy-pagehead__actions">
            <Button onClick={() => void save()} disabled={busy || !dirty} title={h("template.save").tooltip}>
              {h("template.save").label}
            </Button>
          </div>
        )}
      </div>
      <ReviewBanner screen="template-editor" />
      {askOnly && problem && (
        <div className="fy-banner fy-banner--info" role="note">
          <Icon of={UserCheck} size={20} />
          <div className="fy-banner__text">
            <strong>Another person approves this publication</strong>
            <span className="fy-small">{problem.detail} The one who saved the draft never approves it.</span>
          </div>
          {mayPrepare && (
            <div className="fy-banner__action">
              <Button size="sm" variant="primary" onClick={() => void publish(true)} disabled={busy} title={h("template.ask-approval").tooltip}>
                <Icon of={UserCheck} />
                {h("template.ask-approval").label}
              </Button>
            </div>
          )}
        </div>
      )}
      {problem && !askOnly && <Refusal problem={problem} title={failed} />}
      {status && (
        <p className="fy-small" role="status">
          {status}
        </p>
      )}

      <div className="fy-row fy-row--top fy-row--wrap gap-6">
        <div className="fy-stack fy-stack--6 flex-[1_1_340px]">
          <section className="fy-card" aria-labelledby="tpl-name">
            <div className="fy-card__head">
              <h2 className="fy-h3" id="tpl-name">
                Name
              </h2>
            </div>
            <div className="fy-card__body">
              <TextField id="tpl-name-input" label="Template name" value={name} max={100} error={errs.get("name")} onChange={setName} />
            </div>
          </section>

          {t.kind === "email" ? (
            <section className="fy-card" aria-labelledby="mail-title">
              <div className="fy-card__head">
                <h2 className="fy-h3" id="mail-title">
                  The email
                </h2>
              </div>
              <div className="fy-card__body fy-stack fy-stack--3">
                <TextField id="mail-subject" label="Subject" value={content.subject ?? ""} max={200} catalogue={cat} error={errs.get("content.subject")} onChange={(v) => set((c) => (c.subject = v))} />
                <TextField id="mail-body" label="Body" value={content.body ?? ""} max={10000} multiline catalogue={cat} error={errs.get("content.body")} onChange={(v) => set((c) => (c.body = v))} />
              </div>
            </section>
          ) : (
            <>
              <section className="fy-card" aria-labelledby="style-title">
                <div className="fy-card__head">
                  <h2 className="fy-h3" id="style-title">
                    Style
                  </h2>
                </div>
                <div className="fy-card__body fy-stack fy-stack--3">
                  <div className="fy-field fy-only-compact">
                    <label className="fy-label" htmlFor="style-select">
                      Style
                    </label>
                    <select className="fy-select" id="style-select" value={content.theme?.style ?? "classic"} onChange={(e) => set((c) => c.theme && (c.theme.style = e.target.value as StyleKey))}>
                      {styles.map((s) => (
                        <option key={s.key} value={s.key}>
                          {s.name}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="fy-row fy-row--top fy-from-medium flex-nowrap gap-2" role="radiogroup" aria-labelledby="style-title">
                    {styles.map((s) => (
                      <button key={s.key} type="button" role="radio" aria-checked={(content.theme?.style ?? "classic") === s.key} className="fy-choice min-w-0 max-w-[132px] flex-[1_1_0] flex-col items-stretch gap-1.5 p-2" onClick={() => set((c) => c.theme && (c.theme.style = s.key))}>
                        <StyleThumb style={s.key} />
                        <span className="fy-strong">{s.name}</span>
                      </button>
                    ))}
                  </div>
                  <p className="fy-help">{styles.find((s) => s.key === (content.theme?.style ?? "classic"))?.description}. Changing the style keeps your texts and blocks.</p>
                </div>
              </section>

              {content.theme && content.page && (
                <section className="fy-card" aria-labelledby="theme-title">
                  <div className="fy-card__head">
                    <h2 className="fy-h3" id="theme-title">
                      Theme and page
                    </h2>
                  </div>
                  <div className="fy-card__body fy-stack fy-stack--3">
                    <div className="fy-row fy-row--wrap gap-3">
                      <span className="fy-stack fy-grow gap-0">
                        <span className="fy-strong" id="th-logo">
                          Show the organisation's logo
                        </span>
                        <span className="fy-help">From Logo and brand, when it has one</span>
                      </span>
                      <button className="fy-switch" type="button" role="switch" aria-checked={content.theme.logo} aria-labelledby="th-logo" onClick={() => set((c) => c.theme && (c.theme.logo = !c.theme.logo))} />
                    </div>
                    <div className="fy-form">
                      <div className="fy-field">
                        <label className="fy-label" htmlFor="th-accent">
                          Accent colour
                        </label>
                        <input className="fy-input fy-mono" id="th-accent" value={content.theme.accent} maxLength={7} aria-describedby="th-accent-help" aria-invalid={errs.has("content.theme.accent") ? true : undefined} onChange={(e) => set((c) => c.theme && (c.theme.accent = e.target.value.trim()))} />
                        <span className={errs.has("content.theme.accent") || (ratio !== null && ratio < 4.5) ? "fy-error" : "fy-help"} id="th-accent-help">
                          {ratio === null ? "Give it as # and six hexadecimal digits (0 to 9, A to F)." : `${ratio.toFixed(1)}:1 against white: ${ratio >= 4.5 ? "text in it reads" : "too pale for text; choose a darker colour"} (at least 4.5:1).`}
                        </span>
                      </div>
                      <div className="fy-field">
                        <label className="fy-label" htmlFor="th-font">
                          Font
                        </label>
                        <select className="fy-select" id="th-font" value={content.theme.font} onChange={(e) => set((c) => c.theme && (c.theme.font = e.target.value as typeof c.theme.font))}>
                          {cat.fonts.map((f) => (
                            <option key={f}>{f}</option>
                          ))}
                        </select>
                      </div>
                    </div>
                    <div className="fy-row fy-row--wrap gap-5">
                      <div className="fy-stack fy-stack--2">
                        <span className="fy-label" id="th-paper">
                          Paper
                        </span>
                        <div className="fy-segmented" role="radiogroup" aria-labelledby="th-paper">
                          {cat.page_sizes.map((p) => (
                            <button key={p} type="button" role="radio" aria-checked={content.page?.size === p} onClick={() => set((c) => c.page && (c.page.size = p as typeof c.page.size))}>
                              {p}
                            </button>
                          ))}
                        </div>
                      </div>
                      <fieldset className="fy-fieldset flex-[1_1_220px]">
                        <legend className="fy-label">
                          Margins, in mm ({cat.margin_min_mm} to {cat.margin_max_mm})
                        </legend>
                        <div className="fy-row fy-row--wrap gap-2">
                          {(["top", "right", "bottom", "left"] as const).map((side) => (
                            <div key={side} className="fy-field flex-[1_1_56px]">
                              <label className="fy-label fy-small" htmlFor={`th-${side}`}>
                                {side.charAt(0).toUpperCase() + side.slice(1)}
                              </label>
                              <input className="fy-input fy-num" id={`th-${side}`} type="number" inputMode="numeric" min={cat.margin_min_mm} max={cat.margin_max_mm} value={content.page?.margins_mm[side] ?? 20} onChange={(e) => set((c) => c.page && (c.page.margins_mm[side] = Number(e.target.value)))} />
                            </div>
                          ))}
                        </div>
                      </fieldset>
                    </div>
                  </div>
                </section>
              )}

              <section className="fy-card" aria-labelledby="blocks-title">
                <div className="fy-card__head">
                  <h2 className="fy-h3" id="blocks-title">
                    Blocks and texts
                  </h2>
                </div>
                <p className="fy-help px-4 pt-3">Show or hide each block, and write its texts. Locked blocks carry the particulars IRAS requires, so they always show.</p>
                <ol className="fy-list fy-stagger" aria-label="Blocks, in page order">
                  {(content.blocks ?? []).map((b, i) => {
                    const k = kindOf(b.kind);
                    const texts = k?.texts ?? [];
                    const id = `block-${i}`;
                    return (
                      <li key={`${b.kind}-${i}`} className="border-b border-[var(--border)]">
                        <div className="fy-list__item min-h-11 border-b-0">
                          <span className="fy-list__main">
                            <span className="fy-list__title" id={id}>
                              {blockName(b.kind, t.document_type)}
                            </span>
                            <span className="fy-list__meta">{b.shown ? (b.texts?.label ?? b.texts?.title ?? (b.texts?.body ? b.texts.body.slice(0, 48) : "Shown")) : "Hidden"}</span>
                          </span>
                          {texts.length > 0 && (
                            <Button size="sm" variant="ghost" aria-expanded={open === i} aria-controls={`${id}-texts`} onClick={() => setOpen(open === i ? null : i)}>
                              {open === i ? "Done" : "Edit texts"}{" "}
                              <span className="fy-sr">of {blockName(b.kind, t.document_type)}</span>
                            </Button>
                          )}
                          {k?.locked ? (
                            <span className="fy-chip fy-chip--neutral">
                              <Icon of={Lock} />
                              Locked
                            </span>
                          ) : (
                            <button className="fy-switch" type="button" role="switch" aria-checked={b.shown} aria-labelledby={id} onClick={() =>
                                set((c) => {
                                  const x = c.blocks?.[i];
                                  if (x) x.shown = !x.shown;
                                })
                              }
                            />
                          )}
                        </div>
                        {(open === i || texts.some((x) => errs.has(`content.blocks[${i}].texts.${x}`))) && (
                          <div className="fy-stack fy-stack--3 bg-[var(--surface-2)] p-4" id={`${id}-texts`}>
                            {texts.includes("title") && <TextField id={`${id}-title`} label="Title" value={b.texts?.title ?? ""} max={80} error={errs.get(`content.blocks[${i}].texts.title`)} onChange={(v) => setText(i, "title", v)} />}
                            {texts.includes("label") && <TextField id={`${id}-label`} label="Label" value={b.texts?.label ?? ""} max={80} error={errs.get(`content.blocks[${i}].texts.label`)} onChange={(v) => setText(i, "label", v)} />}
                            {texts.includes("body") && <TextField id={`${id}-body`} label="Text" value={b.texts?.body ?? ""} max={2000} multiline catalogue={cat} error={errs.get(`content.blocks[${i}].texts.body`)} onChange={(v) => setText(i, "body", v)} />}
                          </div>
                        )}
                      </li>
                    );
                  })}
                </ol>
                {mode === "expert" && (
                  <div className="fy-card__foot">
                    <span className="fy-tag">Later (P2)</span>
                    <span className="fy-small fy-muted">Moving blocks, per-block layout, conditions and the column chooser</span>
                  </div>
                )}
              </section>
            </>
          )}

          <section className="fy-card" aria-labelledby="fields-title">
            <div className="fy-card__head">
              <h2 className="fy-h3" id="fields-title">
                Fields
              </h2>
              <Button size="sm" variant="ghost" aria-expanded={fieldsShown} aria-controls="tpl-fields" onClick={() => setFieldsShown(!fieldsShown)}>
                {fieldsShown ? "Hide" : `Show all ${cat.fields.length}`}{" "}
                <span className="fy-sr">fields</span>
              </Button>
            </div>
            <div id="tpl-fields" hidden={!fieldsShown}>
            <table className="fy-table fy-table--compact fy-table--cards">
              <thead>
                <tr>
                  <th scope="col">Field</th>
                  <th scope="col">Type</th>
                </tr>
              </thead>
              <tbody>
                {cat.fields.map((f) => (
                  <tr key={f.path}>
                    <td data-card="title">
                      {f.label}
                      {mode === "expert" && <span className="fy-mono fy-small fy-muted block">{f.path}</span>}
                    </td>
                    <td data-card="meta">{f.type === "money" ? "Amount" : f.type === "date" ? "Date" : f.type === "number" ? "Number" : "Text"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            <div className="fy-stack fy-stack--2 border-t border-[var(--border)] px-4 py-3">
              <span className="fy-help">
                <span className="fy-strong">Dates:</span> medium 23 Oct 2026, long 23 October 2026, short 23/10/2026, iso 2026-10-23.
              </span>
              <span className="fy-help">
                <span className="fy-strong">Amounts:</span> code SGD 1,234.50, symbol S$1,234.50, or plain 1,234.50.
              </span>
            </div>
            </div>
          </section>
        </div>

        <div className="fy-stack fy-stack--6 flex-[999_1_460px]">
          <section className="fy-card fy-card--sunken" aria-labelledby="prev-title" aria-busy={preview.busy}>
            <div className="fy-card__head flex-wrap">
              <h2 className="fy-h3" id="prev-title">
                Live preview
              </h2>
              {t.kind === "document" && preview.result && (
                <span className={preview.result.missing_particulars.length ? "fy-chip fy-chip--critical" : "fy-chip fy-chip--good"}>
                  <Icon of={preview.result.missing_particulars.length ? CircleX : CircleCheck} />
                  {preview.result.missing_particulars.length ? `Missing: ${preview.result.missing_particulars.join(", ")}` : "No IRAS particular missing"}
                </span>
              )}
              {pdfLink && (
                <a className="fy-btn fy-btn--ghost fy-btn--sm" href={pdfLink} download={`${t.name}.pdf`}>
                  <Icon of={Download} />
                  Sample PDF
                </a>
              )}
            </div>
            <div className="fy-stack fy-stack--3 p-4">
              {preview.problem && <Refusal problem={preview.problem} title="The preview could not be made" />}
              {!preview.result && !preview.problem && <p className="fy-secondary">Rendering the preview…</p>}
              {preview.result?.pages?.map((png, i) => (
                <img key={i} className="mx-auto w-full max-w-[640px] rounded-sm border border-[var(--border)] shadow-[var(--shadow-2)]" src={`data:image/png;base64,${png}`} alt={`Page ${i + 1} of the ${what.toLowerCase()} with your details and sample data`} />
              ))}
              {t.kind === "email" && preview.result && (
                <article className="fy-stack fy-stack--2 rounded-sm border border-[var(--border)] bg-[var(--surface-1)] p-4" aria-label="The email with sample data">
                  <span className="fy-strong">{preview.result.subject}</span>
                  <p className="whitespace-pre-wrap">{preview.result.body}</p>
                </article>
              )}
              <p className="fy-help">Your details from Settings › Organisation, with a made-up customer and lines. Documents are archival, accessible PDFs, the same bytes every time.</p>
            </div>
          </section>

          <section className="fy-card" aria-labelledby="pub-title">
            <div className="fy-card__head">
              <h2 className="fy-h3" id="pub-title">
                Publish
              </h2>
            </div>
            <div className="fy-card__body fy-stack fy-stack--3">
              {dirty ? (
                <p className="fy-secondary">Save the draft first: publishing makes the saved draft the next version.</p>
              ) : !t.draft_changed ? (
                <p className="fy-secondary">The draft is the version in force: change it to publish another.</p>
              ) : (
                <>
                  <div className="fy-field">
                    <label className="fy-label" htmlFor="pub-note">
                      What changed (optional)
                    </label>
                    <input className="fy-input" id="pub-note" value={note} maxLength={500} onChange={(e) => setNote(e.target.value)} />
                  </div>
                  <div className="fy-row fy-row--wrap gap-2">
                    {mayApprove && (
                      <Button variant="primary" onClick={() => void publish(false)} disabled={busy} title={h("template.publish").tooltip}>
                        <Icon of={Send} />
                        Publish version {(t.published_version ?? 0) + 1}
                      </Button>
                    )}
                    {mayPrepare && !mayApprove && (
                      <Button variant="primary" onClick={() => void publish(true)} disabled={busy} title={h("template.ask-approval").tooltip}>
                        <Icon of={UserCheck} />
                        {h("template.ask-approval").label}
                      </Button>
                    )}
                  </div>
                </>
              )}
              {(t.document_type === "invoice" || t.document_type === "credit_note") && t.kind === "document" && <p className="fy-help">Publishing renders test documents and is refused if one lacks a particular IRAS requires.</p>}
              <p className="fy-help">In Founder mode an Owner or Controller publishes alone and it joins the weekly review; otherwise another person approves. Issued documents keep their version, so a reprint is identical.</p>
            </div>
          </section>

          <section className="fy-card" aria-labelledby="ver-title">
            <div className="fy-card__head">
              <h2 className="fy-h3" id="ver-title">
                Versions
              </h2>
              {mode === "expert" && <span className="fy-tag">Compare side by side later (P2)</span>}
            </div>
            {versions.data && versions.data.length === 0 && <p className="fy-secondary px-4 py-3">No version yet: documents use the Classic starter until one is published.</p>}
            <ul className="fy-list">
              {(versions.data ?? []).map((v) => (
                <li key={v.number} className="fy-list__item">
                  <span className={v.number === t.published_version ? "fy-list__icon fy-list__icon--good" : "fy-list__icon"}>
                    <Icon of={v.number === t.published_version ? CircleCheck : FileClock} size={20} />
                  </span>
                  <span className="fy-list__main">
                    <span className="fy-list__title">Version {v.number}</span>
                    <span className="fy-list__meta">
                      Published{v.published_by?.name ? ` by ${v.published_by.name}` : ""} · {date(v.published_at)}
                      {v.republished_from ? ` · version ${v.republished_from} again` : ""}
                      {v.note ? ` · ${v.note}` : ""}
                      {mode === "expert" && <span className="fy-mono"> · {v.content_hash.slice(0, 8)}</span>}
                    </span>
                  </span>
                  {v.number === t.published_version ? (
                    <span className="fy-chip fy-chip--good">
                      <Icon of={CircleCheck} />
                      In force
                    </span>
                  ) : (
                    mayApprove && (
                      <Button size="sm" variant="ghost" onClick={() => setRollback(v)} disabled={busy}>
                        <Icon of={Undo2} />
                        Roll back <span className="fy-sr">to version {v.number}</span>
                      </Button>
                    )
                  )}
                </li>
              ))}
            </ul>
          </section>
        </div>
      </div>

      <Dialog
        open={rollback !== null}
        onOpenChange={(o) => !o && setRollback(null)}
        dismissible={!busy}
        eyebrow="Templates"
        title={`Roll back to version ${rollback?.number ?? ""}`}
        description={`Version ${rollback?.number ?? ""} is published again as version ${(latest?.number ?? 0) + 1}, and documents render with it. Your draft stays as it is.`}
        footer={
          <>
            <Button variant="ghost" onClick={() => setRollback(null)} disabled={busy}>
              Cancel
            </Button>
            <Button variant="primary" onClick={() => rollback && void publish(false, rollback.number)} disabled={busy} title={h("template.rollback").tooltip}>
              <Icon of={Undo2} />
              {h("template.rollback").label}
            </Button>
          </>
        }
      >
        <div className="fy-field">
          <label className="fy-label" htmlFor="rb-note">
            Why (optional)
          </label>
          <input className="fy-input" id="rb-note" value={note} maxLength={500} onChange={(e) => setNote(e.target.value)} />
        </div>
      </Dialog>
    </div>
  );
}
