import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Copy, LayoutTemplate, Package, Pencil, Plus, Search, TriangleAlert, UserRound } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { allTemplates, customerTypes, supplierTypes, typeNames, type DocType, type Kind } from "../settings/templateWords";

type Contact = components["schemas"]["Contact"];
type Duplicate = components["schemas"]["PossibleDuplicate"];
type Choice = components["schemas"]["DocumentTemplateChoice"];
type Filter = "all" | "customer" | "vendor";

// Who adds and changes contacts and items: C on "Contacts and items" (Appendix D, rev 0.33).
export const contactEditors = ["bookkeeper", "ar_specialist", "ap_specialist", "accountant", "controller"];

async function listContacts(): Promise<Contact[]> {
  const out: Contact[] = [];
  let cursor: string | undefined;
  do {
    const { data, error } = await api.GET("/v1/contacts", { params: { query: { page_size: 500, cursor } } });
    if (error) throw error;
    out.push(...(data.items ?? []));
    cursor = data.next_cursor;
  } while (cursor);
  return out;
}

const textFields = [
  ["legal_name", "Legal name", ""],
  ["uen_or_tax_id", "UEN or tax ID", ""],
  ["billing_email", "Billing email", "Where invoices go"],
  ["remittance_email", "Remittance email", "Where payment advice goes"],
  ["phone", "Telephone", ""],
  ["country", "Country", "Two letters, such as SG"],
  ["tax_residence", "Tax residence", "Only when it is not the country, such as US. A supplier resident outside Singapore may have tax withheld"],
  ["currency", "Currency", "Three letters, such as SGD"],
  ["peppol_id", "Peppol ID", "Such as 0195:SGUEN202312345K"],
  ["default_gst_code", "Default GST code", "Such as TX or SR"],
] as const;

type TextKey = (typeof textFields)[number][0] | "address";
interface Draft extends Record<TextKey, string> {
  customer: boolean;
  vendor: boolean;
  payment_terms_days: string;
  discount_percent: string; // a supplier's early-payment discount (FR-AP-13): blank for none
  discount_days: string;
  active: boolean;
}

const blank: Draft = { legal_name: "", uen_or_tax_id: "", billing_email: "", remittance_email: "", phone: "", country: "SG", tax_residence: "", currency: "SGD", peppol_id: "", default_gst_code: "", address: "", customer: true, vendor: false, payment_terms_days: "", discount_percent: "", discount_days: "", active: true };

function draftOf(c: Contact): Draft {
  const s = (v: string | undefined) => v ?? "";
  return {
    legal_name: c.legal_name,
    uen_or_tax_id: s(c.uen_or_tax_id),
    billing_email: s(c.billing_email),
    remittance_email: s(c.remittance_email),
    phone: s(c.phone),
    country: s(c.country),
    tax_residence: s(c.tax_residence),
    currency: s(c.currency),
    peppol_id: s(c.peppol_id),
    default_gst_code: s(c.default_gst_code),
    address: s(c.address),
    customer: c.customer,
    vendor: c.vendor,
    payment_terms_days: c.payment_terms_days === undefined ? "" : String(c.payment_terms_days),
    discount_percent: s(c.discount_percent),
    discount_days: c.discount_days === undefined ? "" : String(c.discount_days),
    active: c.active,
  };
}

function Duplicates({ list }: { list: Duplicate[] }) {
  return (
    <div className="fy-banner fy-banner--warning" role="alert">
      <Icon of={Copy} size={20} />
      <div className="fy-banner__text">
        <span className="fy-strong">It may be a contact you have already</span>
        {list.map((d) => (
          <span key={d.id} className="fy-small">
            {d.legal_name}: the same {[d.same_name && "name", d.same_uen && "UEN or tax ID", d.same_email && "email"].filter(Boolean).join(", ")}
          </span>
        ))}
        <span className="fy-small">Check before you add it: one company as two contacts splits its balances.</span>
      </div>
    </div>
  );
}

function ContactForm({ editing, onClose, onSaved }: { editing: Contact | "new" | null; onClose: () => void; onSaved: (what: string) => void }) {
  const queries = useQueryClient();
  const id = useId();
  const isNew = editing === "new";
  const current = editing && editing !== "new" ? editing : null;
  const one = useQuery({
    queryKey: ["contact", current?.id],
    enabled: current !== null,
    queryFn: async () => {
      const r = await api.GET("/v1/contacts/{contact_id}", { params: { path: { contact_id: current?.id ?? "" } } });
      if (r.error) throw r.error;
      return { contact: r.data, etag: etagOf(r.response) || `"${r.data.version}"` };
    },
  });
  const [draft, setDraft] = useState<Draft | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [errors, setErrors] = useState<Partial<Record<keyof Draft, string>>>({});
  const [dupes, setDupes] = useState<Duplicate[] | null>(null);
  const base = isNew ? blank : one.data ? draftOf(one.data.contact) : null;
  const d = draft ?? base;
  const set = (k: keyof Draft, v: string | boolean) => {
    if (!d) return;
    setDraft({ ...d, [k]: v });
    if (k === "legal_name" || k === "uen_or_tax_id" || k === "billing_email" || k === "remittance_email") setDupes(null);
  };

  const close = () => {
    setDraft(null);
    setProblem(null);
    setErrors({});
    setDupes(null);
    onClose();
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!d) return;
    const errs: typeof errors = {};
    if (!d.legal_name.trim()) errs.legal_name = "Enter the contact's legal name.";
    if (!d.customer && !d.vendor) errs.customer = "Mark it as a customer, a vendor or both.";
    const terms = d.payment_terms_days.trim();
    if (terms && !(/^\d{1,3}$/.test(terms) && Number(terms) <= 365)) errs.payment_terms_days = "Enter 0 to 365 days, or leave it empty for the organisation's default.";
    const pct = d.discount_percent.trim().replace(/%$/, "").trim();
    const within = d.discount_days.trim();
    if (pct && !/^(0\.(0[1-9]|[1-9]\d?)|[1-9]\d?(\.\d{1,2})?)$/.test(pct)) errs.discount_percent = "Enter the percentage off, above 0 and below 100, such as 2.";
    if (within && !(/^\d{1,3}$/.test(within) && Number(within) >= 1 && Number(within) <= 365)) errs.discount_days = "Enter 1 to 365 days.";
    if (Boolean(pct) !== Boolean(within)) errs[pct ? "discount_days" : "discount_percent"] = "Enter both the percentage and the days, or neither.";
    setErrors(errs);
    if (Object.keys(errs).length) return;
    setBusy(true);
    setProblem(null);
    if (isNew) {
      const body: Record<string, string | number | boolean> = { legal_name: d.legal_name.trim(), customer: d.customer, vendor: d.vendor };
      for (const [k] of textFields) if (k !== "legal_name" && d[k].trim()) body[k] = d[k].trim();
      if (d.address.trim()) body.address = d.address.trim();
      if (terms) body.payment_terms_days = Number(terms);
      if (pct && within) {
        body.discount_percent = pct;
        body.discount_days = Number(within);
      }
      // First a preview, which lists the contacts it may duplicate (FR-CT-04); then, once seen, the add.
      if (dupes === null) {
        const check = await api.POST("/v1/contacts", { params: { header: { Prefer: "return=preview" } }, body: body as never });
        if (!check.data) {
          setBusy(false);
          return setProblem(problemOf(check.error));
        }
        const found = check.data.possible_duplicates ?? [];
        if (found.length) {
          setBusy(false);
          return setDupes(found);
        }
      }
      const out = await api.POST("/v1/contacts", { params: { header: { "Idempotency-Key": newKey() } }, body: body as never });
      setBusy(false);
      if (!out.data) return setProblem(problemOf(out.error));
      await queries.invalidateQueries({ queryKey: ["contacts"] });
      onSaved(`${d.legal_name.trim()} added.`);
      return close();
    }
    if (!one.data || !base) return setBusy(false);
    const changes: Record<string, string | number | boolean> = {};
    for (const k of [...textFields.map(([k]) => k), "address"] as TextKey[]) if (d[k].trim() !== base[k]) changes[k] = d[k].trim();
    for (const k of ["customer", "vendor", "active"] as const) if (d[k] !== base[k]) changes[k] = d[k];
    if (terms !== base.payment_terms_days) changes.payment_terms_days = terms ? Number(terms) : -1; // -1 clears it
    if (pct !== base.discount_percent || within !== base.discount_days) {
      changes.discount_percent = pct; // "" with -1 clears the discount
      changes.discount_days = within ? Number(within) : -1;
    }
    if (Object.keys(changes).length === 0) {
      setBusy(false);
      return close();
    }
    const out = await api.PATCH("/v1/contacts/{contact_id}", { params: { path: { contact_id: one.data.contact.id }, header: { "If-Match": one.data.etag } }, body: changes as never });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await Promise.all(["contacts", "contact"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
    onSaved(`${d.legal_name.trim()} saved.`);
    close();
  };

  const input = (k: keyof Draft, label: string, help: string, type = "text") => (
    <div key={k} className="fy-field">
      <label className="fy-label" htmlFor={`${id}-${k}`}>
        {label}
      </label>
      <input
        className="fy-input"
        id={`${id}-${k}`}
        type={type}
        value={String(d?.[k] ?? "")}
        onChange={(e) => set(k, e.target.value)}
        aria-invalid={errors[k] ? true : undefined}
        aria-describedby={[help && `${id}-${k}-help`, errors[k] && `${id}-${k}-error`].filter(Boolean).join(" ") || undefined}
      />
      {help && (
        <span className="fy-help" id={`${id}-${k}-help`}>
          {help}
        </span>
      )}
      {errors[k] && (
        <span className="fy-error" id={`${id}-${k}-error`}>
          {errors[k]}
        </span>
      )}
    </div>
  );

  return (
    <Dialog
      open={editing !== null}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      wide
      eyebrow="Contacts"
      title={isNew ? "New contact" : current ? current.legal_name : "Contact"}
      description="A customer, a vendor or both. A contact is never deleted: make it inactive to keep its history. Vendor bank details have their own approved change."
    >
      {one.isError && <Refusal problem={problemOf(one.error)} title="The contact could not be read" />}
      {problem && <Refusal problem={problem} title={isNew ? "The contact was not added" : "The contact was not saved"} />}
      {dupes && dupes.length > 0 && <Duplicates list={dupes} />}
      {d && (
        <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
          <fieldset className="fy-fieldset" aria-describedby={errors.customer ? `${id}-roles-error` : undefined}>
            <legend className="fy-label">It is</legend>
            <div className="fy-row gap-4">
              <label className="fy-row gap-2">
                <input type="checkbox" checked={d.customer} onChange={(e) => set("customer", e.target.checked)} />A customer
              </label>
              <label className="fy-row gap-2">
                <input type="checkbox" checked={d.vendor} onChange={(e) => set("vendor", e.target.checked)} />A vendor
              </label>
            </div>
            {errors.customer && (
              <span className="fy-error" id={`${id}-roles-error`}>
                {errors.customer}
              </span>
            )}
          </fieldset>
          <div className="fy-form fy-form--2">
            {textFields.map(([k, label, help]) => input(k, label, help, k.endsWith("email") ? "email" : "text"))}
            {input("payment_terms_days", "Payment terms (days)", "Empty: the organisation's default")}
            {d.vendor && (
              <>
                {input("discount_percent", "Early-payment discount (%)", "A supplier's discount for paying early, such as 2")}
                {input("discount_days", "If paid within (days)", "Of the bill's date, such as 10")}
              </>
            )}
            <div className="fy-field fy-field--full">
              <label className="fy-label" htmlFor={`${id}-address`}>
                Address
              </label>
              <textarea className="fy-textarea" id={`${id}-address`} value={d.address} onChange={(e) => set("address", e.target.value)} />
            </div>
          </div>
          {!isNew && (
            <label className="fy-row gap-2">
              <input type="checkbox" checked={d.active} onChange={(e) => set("active", e.target.checked)} />
              Active: offered for new documents
            </label>
          )}
          <div className="fy-row justify-end gap-2">
            <Button variant="ghost" type="button" onClick={close} disabled={busy}>
              Cancel
            </Button>
            <Button variant="primary" type="submit" disabled={busy}>
              {isNew ? (dupes && dupes.length ? "Add it anyway" : "Add the contact") : "Save the contact"}
            </Button>
          </div>
        </form>
      )}
    </Dialog>
  );
}

// ContactTemplates sets the published template a contact's documents and emails render with, or none, so the
// organisation's default applies (FR-TPL-05; TPL-20): a customer's types for a customer, a supplier's for a vendor.
function ContactTemplates({ contact, onClose, onSaved }: { contact: Contact | null; onClose: () => void; onSaved: (what: string) => void }) {
  const queries = useQueryClient();
  const one = useQuery({
    queryKey: ["contact", contact?.id],
    enabled: contact !== null,
    queryFn: async () => {
      const r = await api.GET("/v1/contacts/{contact_id}", { params: { path: { contact_id: contact?.id ?? "" } } });
      if (r.error) throw r.error;
      return { contact: r.data, etag: etagOf(r.response) || `"${r.data.version}"` };
    },
  });
  const chosen = useQuery({
    queryKey: ["contact-document-templates", contact?.id],
    enabled: contact !== null,
    queryFn: async () => {
      const r = await api.GET("/v1/contacts/{contact_id}/document-templates", { params: { path: { contact_id: contact?.id ?? "" }, query: { page_size: 100 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const list = useQuery({ queryKey: ["document-templates"], queryFn: allTemplates, enabled: contact !== null });
  const [picks, setPicks] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const types = [...(contact?.customer ? customerTypes : []), ...(contact?.vendor ? supplierTypes : [])];
  const kinds: Kind[] = ["document", "email"];
  const inForce = (kind: Kind, type: DocType) => (chosen.data ?? []).find((c: Choice) => c.kind === kind && c.document_type === type)?.template?.id ?? "";
  const pick = (kind: Kind, type: DocType) => picks[`${kind}/${type}`] ?? inForce(kind, type);
  const changed = Object.entries(picks).filter(([k, v]) => {
    const [kind, type] = k.split("/") as [Kind, DocType];
    return v !== inForce(kind, type);
  });
  const close = () => {
    setPicks({});
    setProblem(null);
    onClose();
  };
  const save = async () => {
    if (!contact || !one.data) return;
    setBusy(true);
    setProblem(null);
    for (const [k, v] of changed) {
      const [kind, type] = k.split("/") as [Kind, DocType];
      const out = await api.PUT("/v1/contacts/{contact_id}/document-templates/{kind}/{document_type}", {
        params: { path: { contact_id: contact.id, kind, document_type: type }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } },
        body: v ? { template_id: v } : {},
      });
      if (out.error) {
        setProblem(out.error as ApiProblem);
        setBusy(false);
        await queries.invalidateQueries({ queryKey: ["contact-document-templates", contact.id] });
        return;
      }
    }
    await queries.invalidateQueries({ queryKey: ["contact-document-templates", contact.id] });
    setBusy(false);
    onSaved(`${contact.legal_name}'s templates saved.`);
    close();
  };
  const ready = one.data && chosen.data && list.data;

  return (
    <Dialog
      open={contact !== null}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      wide
      eyebrow="Contacts"
      title={contact ? `Templates for ${contact.legal_name}` : "Templates"}
      description="Their documents and emails render with the template chosen here, before the organisation's default. Only published templates can be chosen."
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void save()} disabled={busy || !ready || changed.length === 0}>
            Save the templates
          </Button>
        </>
      }
    >
      {[one, chosen, list].map((q, i) => q.isError && <Refusal key={i} problem={problemOf(q.error)} title="The templates could not be read" />)}
      {problem && <Refusal problem={problem} title="The templates were not saved" />}
      {!ready && !one.isError && !chosen.isError && !list.isError && <p className="fy-secondary">Loading the templates…</p>}
      {ready && (
        <div className="fy-stack fy-stack--5">
          {types.map((type) => (
            <fieldset key={type} className="fy-fieldset">
              <legend>{typeNames[type]}</legend>
              <div className="fy-form fy-form--2">
                {kinds.map((kind) => {
                  const published = (list.data ?? []).filter((t) => t.kind === kind && t.document_type === type && t.published_version);
                  const field = `ct-${type}-${kind}`;
                  return (
                    <div key={kind} className="fy-field">
                      <label className="fy-label" htmlFor={field}>
                        {kind === "document" ? "Document" : "Email"}
                      </label>
                      <select className="fy-select" id={field} value={pick(kind, type)} disabled={published.length === 0} aria-describedby={published.length === 0 ? `${field}-none` : undefined} onChange={(e) => setPicks({ ...picks, [`${kind}/${type}`]: e.target.value })}>
                        <option value="">The organisation's default</option>
                        {published.map((t) => (
                          <option key={t.id} value={t.id}>
                            {t.name} (version {t.published_version})
                          </option>
                        ))}
                      </select>
                      {published.length === 0 && (
                        <span className="fy-help" id={`${field}-none`}>
                          No {kind === "document" ? typeNames[type].toLowerCase() : "email"} template is published yet.
                        </span>
                      )}
                    </div>
                  );
                })}
              </div>
            </fieldset>
          ))}
        </div>
      )}
    </Dialog>
  );
}

// Contacts (docs/design/screens Contacts; FR-CT-01, FR-CT-04): the customers and vendors, filtered by
// role and found by name, UEN or email, each with its details and a flag when it may duplicate
// another. Balances, ageing and the activity timeline join with the sales and purchase documents.
export function Contacts() {
  const { session } = useSignedIn();
  const q = useQuery({ queryKey: ["contacts"], queryFn: listContacts });
  const [filter, setFilter] = useState<Filter>("all");
  const [find, setFind] = useState("");
  const [inactive, setInactive] = useState(false);
  const [editing, setEditing] = useState<Contact | "new" | null>(null);
  const [templating, setTemplating] = useState<Contact | null>(null);
  const [status, setStatus] = useState("");
  const findId = useId();
  const may = (session.roles ?? []).some((r) => contactEditors.includes(r));
  const add = helpText("contact.add");
  const templates = helpText("contact.templates");
  const all = q.data ?? [];
  const needle = find.trim().toLowerCase();
  const shown = all
    .filter((c) => (inactive || c.active) && (filter === "all" || c[filter]))
    .filter((c) => !needle || [c.legal_name, c.uen_or_tax_id, c.billing_email, c.remittance_email].some((v) => v?.toLowerCase().includes(needle)))
    .sort((a, b) => a.legal_name.localeCompare(b.legal_name));
  const count = (f: Filter) => all.filter((c) => c.active && (f === "all" || c[f])).length;

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">Contacts</h1>
          <p className="fy-secondary">The customers and vendors the company deals with.</p>
        </div>
        <div className="fy-pagehead__actions">
          <Link className="fy-btn fy-btn--ghost" to="/items">
            <Icon of={Package} />
            Items and prices
          </Link>
          {may && (
            <Button variant="primary" onClick={() => setEditing("new")} title={add.tooltip}>
              <Icon of={Plus} />
              {add.label}
            </Button>
          )}
        </div>
      </div>
      <p className="fy-small" role="status">
        {status}
      </p>
      <div className="fy-row fy-row--wrap gap-3">
        <div className="fy-chipset" role="group" aria-label="Show">
          {(
            [
              ["all", "All"],
              ["customer", "Customers"],
              ["vendor", "Vendors"],
            ] as const
          ).map(([f, label]) => (
            <button key={f} type="button" className="fy-filter" aria-pressed={filter === f} onClick={() => setFilter(f)}>
              {label} {q.data ? count(f) : ""}
            </button>
          ))}
        </div>
        <div className="fy-inputgroup max-w-80 flex-1">
          <span className="fy-inputgroup__addon">
            <Icon of={Search} />
          </span>
          <label className="fy-sr" htmlFor={findId}>
            Find a contact
          </label>
          <input className="fy-input" id={findId} type="search" placeholder="Name, UEN or email" value={find} onChange={(e) => setFind(e.target.value)} />
        </div>
        <label className="fy-row gap-2 fy-small">
          <input type="checkbox" checked={inactive} onChange={(e) => setInactive(e.target.checked)} />
          Show inactive contacts
        </label>
      </div>
      {q.isError && <Refusal problem={problemOf(q.error)} title="The contacts could not be shown" />}
      {q.isPending && <p className="fy-secondary">Loading the contacts…</p>}
      {q.data && all.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={UserRound} size={24} />
          <h2 className="fy-h3">No contacts yet</h2>
          <p className="fy-secondary">Add the customers you invoice and the vendors whose bills you pay. Fyniq flags one that may duplicate another.</p>
          {!may && <p className="fy-small fy-secondary">A Bookkeeper, an AR or AP specialist, an Accountant or a Controller adds them.</p>}
        </div>
      )}
      {all.length > 0 && (
        <section className="fy-card" aria-labelledby="contacts-title">
          <h2 className="fy-sr" id="contacts-title">
            Contacts shown
          </h2>
          {shown.length === 0 ? (
            <p className="fy-card__body fy-secondary">No contact matches.</p>
          ) : (
            <ul className="fy-list fy-stagger" aria-labelledby="contacts-title">
              {shown.map((c) => (
                <li key={c.id} className="fy-list__item">
                  <span className="fy-avatar fy-avatar--sm" aria-hidden="true">
                    {c.legal_name.slice(0, 2).toUpperCase()}
                  </span>
                  <span className="fy-list__main">
                    <span className="fy-list__title whitespace-normal">{c.legal_name}</span>
                    <span className="fy-list__meta whitespace-normal">
                      {[c.customer && "Customer", c.vendor && "Vendor"].filter(Boolean).join(" and ")}
                      {c.uen_or_tax_id ? ` · ${c.uen_or_tax_id}` : ""}
                      {c.billing_email ? ` · ${c.billing_email}` : ""}
                      {c.payment_terms_days !== undefined ? ` · ${c.payment_terms_days} days` : ""}
                    </span>
                  </span>
                  {(c.possible_duplicates ?? []).length > 0 && (
                    <span className="fy-chip fy-chip--warning" title={`May duplicate ${(c.possible_duplicates ?? []).map((d) => d.legal_name).join(", ")}`}>
                      <Icon of={TriangleAlert} />
                      Possible duplicate
                    </span>
                  )}
                  {!c.active && <span className="fy-chip fy-chip--neutral">Inactive</span>}
                  {may && (
                    <Button variant="ghost" size="sm" onClick={() => setTemplating(c)} aria-label={`Templates for ${c.legal_name}`} title={templates.tooltip}>
                      <Icon of={LayoutTemplate} />
                    </Button>
                  )}
                  {may && (
                    <Button variant="ghost" size="sm" onClick={() => setEditing(c)} aria-label={`Change ${c.legal_name}`}>
                      <Icon of={Pencil} />
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </section>
      )}
      <ContactForm editing={editing} onClose={() => setEditing(null)} onSaved={setStatus} />
      <ContactTemplates contact={templating} onClose={() => setTemplating(null)} onSaved={setStatus} />
    </div>
  );
}
