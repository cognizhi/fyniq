import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Contact = components["schemas"]["EmergencyContact"];
type Note = { ok: boolean; text: string } | null;
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("details.failed");
const kinds = ["legal_name", "residency", "nric_fin"] as const;
const residencies = ["citizen", "permanent_resident", "foreigner"] as const;
type Change = React.ChangeEvent<HTMLInputElement>;
const blank: Contact = { name: "", relationship: "", phone: "" };

// bytesOf reads a chosen file as base64 for the API, without keeping it anywhere.
async function base64Of(file: File): Promise<string> {
  const buf = new Uint8Array(await file.arrayBuffer());
  let s = "";
  buf.forEach((b) => (s += String.fromCharCode(b)));
  return btoa(s);
}

// MyDetails is nm-my-details: a person changes their phone, personal email, address and emergency contacts at once, and asks the people team to change their legal name,
// residency or NRIC or FIN with evidence (NM-ESS-03). A change that needs approval never holds an NRIC or FIN number here: the note says what changed.
export function MyDetails({ roles }: { roles: readonly string[] }) {
  const [d, setD] = useState({ phone: "", personal_email: "", address: "", postal_code: "" });
  const [contacts, setContacts] = useState<Contact[]>([]);
  const [note, setNote] = useState<Note>(null);
  const [change, setChange] = useState({ kind: "legal_name" as (typeof kinds)[number], legal_name: "", residency: "citizen" as (typeof residencies)[number], pr_start: "", note: "", title: "" });
  const [file, setFile] = useState<File | null>(null);
  useEffect(() => {
    api.GET("/v1/me/details").then(
      ({ data }) => {
        if (!data) return;
        setD({ phone: data.phone ?? "", personal_email: data.personal_email ?? "", address: data.address ?? "", postal_code: data.postal_code ?? "" });
        setContacts(data.emergency_contacts ?? []);
      },
      () => setNote({ ok: false, text: t("details.failed") }),
    );
  }, []);
  const saveDetails = async (e: React.FormEvent) => {
    e.preventDefault();
    const { error } = await api.PUT("/v1/me/details", { body: d });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("details.saved") });
  };
  const saveContacts = async (e: React.FormEvent) => {
    e.preventDefault();
    const { error } = await api.PUT("/v1/me/emergency-contacts", { body: { contacts } });
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("details.savedContacts") });
  };
  const edit = (i: number, k: keyof Contact, v: string) => setContacts(contacts.map((c, j) => (j === i ? { ...c, [k]: v } : c)));
  const ask = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!file) return setNote({ ok: false, text: t("details.nofile") });
    const { data, error } = await api.POST("/v1/me/details-changes", {
      body: {
        kind: change.kind,
        legal_name: change.kind === "legal_name" ? change.legal_name : undefined,
        residency: change.kind === "residency" ? change.residency : undefined,
        pr_start: change.kind === "residency" && change.residency === "permanent_resident" ? change.pr_start : undefined,
        note: change.note,
        evidence: { title: change.title || file.name, content_type: file.type || "application/octet-stream", content_base64: await base64Of(file) },
      },
    });
    setNote(data ? { ok: true, text: t("details.asked", { due: data.due_on }) } : { ok: false, text: problem(error) });
  };
  const set = (k: keyof typeof d) => (e: Change) => setD({ ...d, [k]: e.target.value });
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-my-details" />
        <h1 className="fy-h1" title={helpText("details.page").tooltip}>{t("details.title")}</h1>
        <p className="fy-secondary">{t("details.lead")}</p>
        <form className="fy-stack fy-stack--2" onSubmit={saveDetails} aria-label={t("details.contact")}>
          <h2 className="fy-h2">{t("details.contact")}</h2>
          <label className="fy-field"><span>{t("details.phone")}</span><input className="fy-input" type="tel" maxLength={30} value={d.phone} onChange={set("phone")} /></label>
          <label className="fy-field"><span>{t("details.email")}</span><input className="fy-input" type="email" maxLength={200} value={d.personal_email} onChange={set("personal_email")} /></label>
          <label className="fy-field"><span>{t("details.address")}</span><input className="fy-input" maxLength={300} value={d.address} onChange={set("address")} /></label>
          <label className="fy-field"><span>{t("details.postal")}</span><input className="fy-input" maxLength={12} value={d.postal_code} onChange={set("postal_code")} /></label>
          <button className="fy-btn fy-btn--primary" type="submit">{t("details.save")}</button>
        </form>
        <form className="fy-stack fy-stack--2" onSubmit={saveContacts} aria-label={t("details.emergency")}>
          <h2 className="fy-h2">{t("details.emergency")}</h2>
          {contacts.map((c, i) => (
            <fieldset key={i} className="fy-stack fy-stack--1">
              <legend>{t("details.person", { n: String(i + 1) })}</legend>
              <label className="fy-field"><span>{t("details.cname", { n: String(i + 1) })}</span><input className="fy-input" required maxLength={120} value={c.name} onChange={(e) => edit(i, "name", e.target.value)} /></label>
              <label className="fy-field"><span>{t("details.relationship", { n: String(i + 1) })}</span><input className="fy-input" required maxLength={60} value={c.relationship} onChange={(e) => edit(i, "relationship", e.target.value)} /></label>
              <label className="fy-field"><span>{t("details.cphone", { n: String(i + 1) })}</span><input className="fy-input" type="tel" required maxLength={30} value={c.phone} onChange={(e) => edit(i, "phone", e.target.value)} /></label>
              <button className="fy-btn fy-btn--secondary" type="button" onClick={() => setContacts(contacts.filter((_, j) => j !== i))}>{t("details.remove", { n: String(i + 1) })}</button>
            </fieldset>
          ))}
          {contacts.length < 3 && <button className="fy-btn fy-btn--secondary" type="button" onClick={() => setContacts([...contacts, blank])}>{t("details.add")}</button>}
          <button className="fy-btn fy-btn--primary" type="submit">{t("details.saveContacts")}</button>
        </form>
        <form className="fy-stack fy-stack--2" onSubmit={ask} aria-label={t("details.change")}>
          <h2 className="fy-h2">{t("details.change")}</h2>
          <p className="fy-secondary">{t("details.changeLead")}</p>
          <label className="fy-field">
            <span>{t("details.kind")}</span>
            <select className="fy-input" value={change.kind} onChange={(e) => setChange({ ...change, kind: e.target.value as (typeof kinds)[number] })}>
              {kinds.map((k) => <option key={k} value={k}>{t(`steps.change.${k}`)}</option>)}
            </select>
          </label>
          {change.kind === "legal_name" && <label className="fy-field"><span>{t("details.newname")}</span><input className="fy-input" required maxLength={200} value={change.legal_name} onChange={(e) => setChange({ ...change, legal_name: e.target.value })} /></label>}
          {change.kind === "residency" && (
            <>
              <label className="fy-field">
                <span>{t("details.residency")}</span>
                <select className="fy-input" value={change.residency} onChange={(e) => setChange({ ...change, residency: e.target.value as (typeof residencies)[number] })}>
                  {residencies.map((r) => <option key={r} value={r}>{t(`value.${r}`)}</option>)}
                </select>
              </label>
              {change.residency === "permanent_resident" && <label className="fy-field"><span>{t("details.prstart")}</span><input className="fy-input" type="date" required value={change.pr_start} onChange={(e) => setChange({ ...change, pr_start: e.target.value })} /></label>}
            </>
          )}
          {change.kind === "nric_fin" && <p className="fy-secondary">{t("details.nricNote")}</p>}
          <label className="fy-field"><span>{t("details.note")}</span><input className="fy-input" required maxLength={500} value={change.note} onChange={(e) => setChange({ ...change, note: e.target.value })} /></label>
          <label className="fy-field"><span>{t("details.evidence")}</span><input className="fy-input" type="file" required onChange={(e) => setFile(e.target.files?.[0] ?? null)} /></label>
          <button className="fy-btn fy-btn--primary" type="submit">{t("details.ask")}</button>
        </form>
        {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
        <a href="/me/requests">{t("letters.mine")}</a>
      </main>
    </Shell>
  );
}
