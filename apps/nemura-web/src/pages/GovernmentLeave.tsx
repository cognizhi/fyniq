import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

type Block = components["schemas"]["GovBlock"];
type Claim = components["schemas"]["GovClaim"];
type Entry = components["schemas"]["DirectoryEntry"];
type Child = components["schemas"]["Dependant"];
type Note = { ok: boolean; text: string } | null;
type State = { status: "loading" } | { status: "failed" } | { status: "ready"; blocks: Block[] };
const problem = (e: unknown): string => (e as { detail?: string } | undefined)?.detail ?? t("gov.failed");
const schemes = ["maternity", "paternity", "shared_parental"] as const;

// GovernmentLeave is nm-govt-claims: each block of maternity, paternity or shared parental leave with its claim deadline and the employer's claims, apart from the
// salary owed (NM-LV-07, NM-LV-14). The claim is filed on the portal by a person; this page keeps the record. The API applies the pack's rules and the caps.
export function GovernmentLeave({ roles }: { roles: readonly string[] }) {
  const [state, set] = useState<State>({ status: "loading" });
  const [note, setNote] = useState<Note>(null);
  const writes = roles.some((r) => ["hr_officer", "hr_admin"].includes(r));
  const load = async () => {
    try {
      const { data } = await api.GET("/v1/government-paid-leave");
      set(data ? { status: "ready", blocks: data.blocks ?? [] } : { status: "failed" });
    } catch {
      set({ status: "failed" });
    }
  };
  useEffect(() => {
    void load();
  }, []);
  const done = async (error: unknown) => {
    setNote(error ? { ok: false, text: problem(error) } : { ok: true, text: t("gov.saved") });
    if (!error) await load();
  };
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-govt-claims" />
        <h1 className="fy-h1" title={helpText("gov.page").tooltip}>{t("gov.title")}</h1>
        <p className="fy-secondary">{t("gov.lead")}</p>
        {state.status === "loading" && <p role="status" className="fy-secondary">{t("gov.loading")}</p>}
        {state.status === "failed" && <p role="alert" className="fy-secondary">{t("gov.failed")}</p>}
        {note && <p role={note.ok ? "status" : "alert"} className="fy-secondary">{note.text}</p>}
        {state.status === "ready" && state.blocks.length === 0 && <p className="fy-secondary">{t("gov.none")}</p>}
        {state.status === "ready" && state.blocks.map((b) => <BlockView key={b.id} b={b} writes={writes} done={done} />)}
        {writes && <RecordForm done={done} />}
      </main>
    </Shell>
  );
}

function BlockView({ b, writes, done }: { b: Block; writes: boolean; done: (e: unknown) => unknown }) {
  const [amount, setAmount] = useState("");
  const [filed, setFiled] = useState("");
  const [reference, setReference] = useState("");
  const claims = b.claims ?? [];
  const file = async (e: React.FormEvent) => {
    e.preventDefault();
    const { error } = await api.POST("/v1/government-paid-leave/{id}/claims", { params: { path: { id: b.id } }, body: { amount, filed_on: filed, reference } });
    await done(error);
  };
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby={`b-${b.id}`}>
      <h2 className="fy-h2" id={`b-${b.id}`}>{t("gov.block", { person: b.person_name, scheme: t(`gov.scheme.${b.scheme}`), from: b.from, to: b.to, days: String(b.days) })}</h2>
      <p className="fy-secondary">{t("gov.deadline", { date: b.claim_deadline })}</p>
      {b.guide_maximum && <p className="fy-muted">{t("gov.guide", { amount: b.guide_maximum })}</p>}
      <h3 className="fy-h3">{t("gov.claims")}</h3>
      {claims.length === 0 && <p className="fy-secondary">{t("gov.noClaims")}</p>}
      <ul className="fy-stack fy-stack--1">
        {claims.map((c) => (
          <li key={c.id}>
            <ClaimLine c={c} writes={writes} done={done} />
          </li>
        ))}
      </ul>
      {writes && (
        <form className="fy-stack fy-stack--1" onSubmit={file} aria-label={`${t("gov.fileTitle")} ${b.id}`}>
          <label className="fy-field">
            <span>{t("gov.amount")}</span>
            <input className="fy-input" inputMode="decimal" required value={amount} onChange={(e) => setAmount(e.target.value)} />
          </label>
          <label className="fy-field">
            <span>{t("gov.filedOn")}</span>
            <input className="fy-input" type="date" required value={filed} onChange={(e) => setFiled(e.target.value)} />
          </label>
          <label className="fy-field">
            <span>{t("gov.reference")}</span>
            <input className="fy-input" maxLength={100} value={reference} onChange={(e) => setReference(e.target.value)} />
          </label>
          <button className="fy-btn fy-btn--secondary" type="submit">{t("gov.file")}</button>
        </form>
      )}
    </section>
  );
}

function ClaimLine({ c, writes, done }: { c: Claim; writes: boolean; done: (e: unknown) => unknown }) {
  const [kind, setKind] = useState<"receipt" | "rejection">("receipt");
  const [amount, setAmount] = useState("");
  const [on, setOn] = useState("");
  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    const { error } = await api.POST("/v1/government-paid-claims/{id}/events", { params: { path: { id: c.id } }, body: { kind, amount: kind === "receipt" ? amount : undefined, on } });
    await done(error);
  };
  return (
    <div className="fy-stack fy-stack--1">
      <p>
        {t("gov.claim", { amount: c.amount, on: c.filed_on, received: c.received, state: t(`gov.state.${c.state}`) })} {c.over_guide && <strong>{t("gov.over")}</strong>}
      </p>
      {writes && c.state !== "paid" && (
        <form className="fy-row" style={{ gap: "var(--space-2)", flexWrap: "wrap" }} onSubmit={save} aria-label={t("gov.eventFor", { on: c.filed_on })}>
          <select className="fy-input" aria-label={t("gov.eventKind")} value={kind} onChange={(e) => setKind(e.target.value as "receipt" | "rejection")}>
            <option value="receipt">{t("gov.event.receipt")}</option>
            <option value="rejection">{t("gov.event.rejection")}</option>
          </select>
          {kind === "receipt" && <input className="fy-input" aria-label={t("gov.eventAmount")} inputMode="decimal" required value={amount} onChange={(e) => setAmount(e.target.value)} />}
          <input className="fy-input" aria-label={t("gov.eventOn")} type="date" required value={on} onChange={(e) => setOn(e.target.value)} />
          <button className="fy-btn fy-btn--secondary" type="submit">{t("gov.eventSave")}</button>
        </form>
      )}
    </div>
  );
}

function RecordForm({ done }: { done: (e: unknown) => unknown }) {
  const [people, setPeople] = useState<Entry[]>([]);
  const [children, setChildren] = useState<Child[]>([]);
  const [person, setPerson] = useState("");
  const [child, setChild] = useState("");
  const [scheme, setScheme] = useState<(typeof schemes)[number]>("paternity");
  const [order, setOrder] = useState("1");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  useEffect(() => {
    api.GET("/v1/people").then(({ data }) => setPeople(data?.people ?? []), () => setPeople([]));
  }, []);
  useEffect(() => {
    setChild("");
    setChildren([]);
    if (!person) return;
    api.GET("/v1/people/{id}/dependants", { params: { path: { id: person } } }).then(({ data }) => setChildren(data?.dependants ?? []), () => setChildren([]));
  }, [person]);
  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    const { error } = await api.POST("/v1/people/{id}/government-paid-leave", { params: { path: { id: person } }, body: { scheme, dependant_id: child, child_order: Number(order), from, to } });
    await done(error);
  };
  return (
    <form className="fy-stack fy-stack--2" onSubmit={save} aria-labelledby="record-h">
      <h2 className="fy-h2" id="record-h">{t("gov.recordTitle")}</h2>
      <label className="fy-field">
        <span>{t("gov.person")}</span>
        <select className="fy-input" required value={person} onChange={(e) => setPerson(e.target.value)}>
          <option value="">{t("gov.choose")}</option>
          {people.map((p) => (
            <option key={p.id} value={p.id}>{p.name}</option>
          ))}
        </select>
      </label>
      <label className="fy-field">
        <span>{t("gov.child")}</span>
        <select className="fy-input" required value={child} onChange={(e) => setChild(e.target.value)}>
          <option value="">{t("gov.choose")}</option>
          {children.map((c) => (
            <option key={c.id} value={c.id}>{t("gov.childLine", { relationship: c.relationship, born: c.birth_date ?? "" })}</option>
          ))}
        </select>
      </label>
      <label className="fy-field">
        <span>{t("gov.scheme")}</span>
        <select className="fy-input" value={scheme} onChange={(e) => setScheme(e.target.value as (typeof schemes)[number])}>
          {schemes.map((s) => (
            <option key={s} value={s}>{t(`gov.scheme.${s}`)}</option>
          ))}
        </select>
      </label>
      <label className="fy-field">
        <span>{t("gov.order")}</span>
        <input className="fy-input" type="number" min={1} max={20} required value={order} onChange={(e) => setOrder(e.target.value)} />
      </label>
      <label className="fy-field">
        <span>{t("gov.from")}</span>
        <input className="fy-input" type="date" required value={from} onChange={(e) => setFrom(e.target.value)} />
      </label>
      <label className="fy-field">
        <span>{t("gov.to")}</span>
        <input className="fy-input" type="date" required value={to} onChange={(e) => setTo(e.target.value)} />
      </label>
      <button className="fy-btn fy-btn--primary" type="submit">{t("gov.record")}</button>
    </form>
  );
}
