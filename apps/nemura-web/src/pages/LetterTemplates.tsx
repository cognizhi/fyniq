import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { Delegations } from "../components/Delegations";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Type = components["schemas"]["LetterType"];
type Field = components["schemas"]["LetterField"];
type Version = components["schemas"]["LetterTemplate"];
type Block = components["schemas"]["LetterBlock"];
type Note = { ok: boolean; text: string } | null;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("tpl.failed");
const kinds = ["letterhead", "heading", "paragraph", "signature"] as const;

// LetterTemplates is nm-letter-template: the letterhead, which letter types the company issues, and each type's template as blocks and merge fields: a draft until the HR
// administrator approves it, then an immutable numbered version (NM-LTR-01, NM-LTR-02). The API checks every block and field and reports every problem at once.
export function LetterTemplates({ roles }: { roles: readonly string[] }) {
  const [types, setTypes] = useState<Type[]>([]);
  const [fields, setFields] = useState<Field[]>([]);
  const [chosen, setChosen] = useState("");
  const [note, setNote] = useState<Note>(null);
  const approver = roles.some((r) => ["hr_admin", "owner"].includes(r));
  const load = async () => {
    try {
      const { data } = await api.GET("/v1/letters/types");
      setTypes(data?.types ?? []);
      setFields(data?.fields ?? []);
    } catch {
      setTypes([]);
    }
  };
  useEffect(() => {
    void load();
  }, []);
  const type = types.find((x) => x.key === chosen);
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-letter-template" />
        <h1 className="fy-h1" title={helpText("tpl.page").tooltip}>{t("tpl.title")}</h1>
        <p className="fy-secondary">{t("tpl.lead")}</p>
        {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
        {approver && <Letterhead report={setNote} />}
        <section className="fy-stack fy-stack--2" aria-labelledby="types-h">
          <h2 className="fy-h2" id="types-h">{t("tpl.types")}</h2>
          <ul className="fy-stack fy-stack--2">
            {types.map((x) => (
              <TypeRow key={x.key} x={x} editable={approver} report={setNote} saved={load} />
            ))}
          </ul>
        </section>
        {approver && <Delegations types={types} signatory={roles.includes("owner")} />}
        <section className="fy-stack fy-stack--2" aria-labelledby="editor-h">
          <label className="fy-field">
            <span>{t("tpl.choose")}</span>
            <select className="fy-input" value={chosen} onChange={(e) => setChosen(e.target.value)}>
              <option value="">–</option>
              {types.filter((x) => !x.needs).map((x) => (
                <option key={x.key} value={x.key}>{x.title}</option>
              ))}
            </select>
          </label>
          {type && <Editor key={type.key} type={type} fields={fields} approver={approver} report={setNote} saved={load} />}
        </section>
      </main>
    </Shell>
  );
}

function Letterhead({ report }: { report: (n: Note) => void }) {
  const [name, setName] = useState("");
  const [uen, setUen] = useState("");
  const [address, setAddress] = useState("");
  useEffect(() => {
    api.GET("/v1/letters/settings").then(({ data }) => {
      if (data?.set) {
        setName(data.settings.company_name);
        setUen(data.settings.uen);
        setAddress(data.settings.address);
      }
    }, () => undefined);
  }, []);
  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    const { error } = await api.PUT("/v1/letters/settings", { body: { company_name: name, uen, address } });
    report(error ? { ok: false, text: problem(error) } : { ok: true, text: t("tpl.saved") });
  };
  return (
    <form className="fy-stack fy-stack--2" onSubmit={save} aria-labelledby="head-h">
      <h2 className="fy-h2" id="head-h">{t("tpl.letterhead")}</h2>
      <label className="fy-field">
        <span>{t("tpl.name")}</span>
        <input className="fy-input" required maxLength={200} value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      <label className="fy-field">
        <span>{t("tpl.uen")}</span>
        <input className="fy-input" required maxLength={30} value={uen} onChange={(e) => setUen(e.target.value)} />
      </label>
      <label className="fy-field">
        <span>{t("tpl.address")}</span>
        <input className="fy-input" required maxLength={400} value={address} onChange={(e) => setAddress(e.target.value)} />
      </label>
      <button className="fy-btn fy-btn--secondary" type="submit">{t("tpl.saveHead")}</button>
    </form>
  );
}

function TypeRow({ x, editable, report, saved }: { x: Type; editable: boolean; report: (n: Note) => void; saved: () => unknown }) {
  const [on, setOn] = useState(x.enabled);
  const [days, setDays] = useState(String(x.due_days));
  const save = async () => {
    const { error } = await api.PUT("/v1/letters/types/{key}", { params: { path: { key: x.key } }, body: { enabled: on, due_days: Number(days) } });
    report(error ? { ok: false, text: problem(error) } : { ok: true, text: t("tpl.saved") });
    if (!error) await saved();
  };
  return (
    <li className="fy-stack fy-stack--1">
      <strong>{x.title}</strong>
      <span className="fy-secondary">{x.statement}</span>
      {x.needs ? (
        <span className="fy-muted">{t("tpl.needs", { needs: x.needs })}</span>
      ) : (
        <div className="fy-row" style={{ gap: "var(--space-3)", flexWrap: "wrap" }}>
          <label className="fy-row" style={{ gap: "var(--space-1)" }}>
            <input type="checkbox" checked={on} disabled={!editable} onChange={(e) => setOn(e.target.checked)} />
            <span>{t("tpl.on")}: {x.title}</span>
          </label>
          <label className="fy-field">
            <span>{t("tpl.days")}: {x.title}</span>
            <input className="fy-input" type="number" min={1} max={30} disabled={!editable} value={days} onChange={(e) => setDays(e.target.value)} />
          </label>
          {editable && <button className="fy-btn fy-btn--secondary" type="button" onClick={save}>{t("tpl.saveType")}</button>}
          <span className="fy-muted">{t("request.version", { version: String(x.published_version) })}</span>
        </div>
      )}
    </li>
  );
}

function Editor({ type, fields, approver, report, saved }: { type: Type; fields: Field[]; approver: boolean; report: (n: Note) => void; saved: () => unknown }) {
  const [versions, setVersions] = useState<Version[]>([]);
  const [blocks, setBlocks] = useState<Block[]>([{ kind: "paragraph", text: "" }]);
  const [noteText, setNoteText] = useState("");
  const load = async () => {
    const { data } = await api.GET("/v1/letters/types/{key}/templates", { params: { path: { key: type.key } } });
    const v = data?.versions ?? [];
    setVersions(v);
    const start = v.find((x) => x.state === "draft") ?? v.find((x) => x.state === "published");
    if (start) setBlocks((start.blocks ?? []).map((b) => ({ kind: b.kind, text: b.text })));
  };
  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [type.key]);
  const draft = versions.find((v) => v.state === "draft");
  const save = async () => {
    const { error } = await api.PUT("/v1/letters/types/{key}/template", { params: { path: { key: type.key } }, body: { blocks, note: noteText } });
    report(error ? { ok: false, text: problem(error) } : { ok: true, text: t("tpl.saved") });
    if (!error) await load();
  };
  const publish = async () => {
    const { data, error } = await api.POST("/v1/letters/templates/{id}/publication", { params: { path: { id: draft?.id ?? "" } } });
    report(data ? { ok: true, text: t("tpl.published", { version: String(data.version) }) } : { ok: false, text: problem(error) });
    if (data) {
      await load();
      await saved();
    }
  };
  const update = (i: number, patch: Partial<Block>) => setBlocks(blocks.map((b, j) => (j === i ? { ...b, ...patch } : b)));
  return (
    <div className="fy-stack fy-stack--2">
      <h2 className="fy-h2" id="editor-h">{t("tpl.editor", { type: type.title })}</h2>
      <p className="fy-muted">{t("tpl.fields", { fields: fields.filter((f) => (type.fields ?? []).includes(f.key)).map((f) => `{{${f.key}}}`).join(", ") })}</p>
      {blocks.map((b, i) => (
        <div key={i} className="fy-stack fy-stack--1">
          <label className="fy-field">
            <span>{t("tpl.kind", { n: String(i + 1) })}</span>
            <select className="fy-input" value={b.kind} onChange={(e) => update(i, { kind: e.target.value as Block["kind"] })}>
              {kinds.map((k) => (
                <option key={k} value={k}>{t(`tpl.block.${k}`)}</option>
              ))}
            </select>
          </label>
          <label className="fy-field">
            <span>{t("tpl.text", { n: String(i + 1) })}</span>
            <textarea className="fy-input" maxLength={2000} value={b.text} onChange={(e) => update(i, { text: e.target.value })} />
          </label>
          <button className="fy-btn fy-btn--secondary" type="button" onClick={() => setBlocks(blocks.filter((_, j) => j !== i))}>{t("tpl.removeBlock", { n: String(i + 1) })}</button>
        </div>
      ))}
      <div className="fy-row" style={{ gap: "var(--space-2)", flexWrap: "wrap" }}>
        <button className="fy-btn fy-btn--secondary" type="button" onClick={() => setBlocks([...blocks, { kind: "paragraph", text: "" }])}>{t("tpl.addBlock")}</button>
        <button className="fy-btn fy-btn--primary" type="button" onClick={save}>{t("tpl.saveDraft")}</button>
        {approver && draft && <button className="fy-btn fy-btn--primary" type="button" onClick={publish}>{t("tpl.publish")}</button>}
      </div>
      <h3 className="fy-h3">{t("tpl.versions")}</h3>
      <ul className="fy-stack fy-stack--1">
        {versions.map((v) => (
          <li key={v.id}>{v.state === "draft" ? t("tpl.draftLine") : t("tpl.versionLine", { version: String(v.version), state: v.state })}{v.note ? `: ${v.note}` : ""}</li>
        ))}
      </ul>
    </div>
  );
}
