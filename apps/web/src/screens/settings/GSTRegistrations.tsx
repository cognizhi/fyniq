import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, CircleMinus, Pencil, Plus } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date, timeZone } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Registration = components["schemas"]["GSTRegistration"];

// quarterMonths names the months a registration's quarters start in, by its first.
export const quarterMonths: Record<number, string> = {
  1: "January, April, July and October",
  2: "February, May, August and November",
  3: "March, June, September and December",
};

// today is the organisation's date today, YYYY-MM-DD.
function today(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
}

async function listRegistrations() {
  const { data, error } = await api.GET("/v1/organisation/gst-registrations", { params: { query: { page_size: 100 } } });
  if (error) throw error;
  return data.items ?? [];
}

async function statusToday() {
  const { data, error } = await api.GET("/v1/organisation/gst-status", { params: { query: { date: today() } } });
  if (error) throw error;
  return data;
}

// returns says how often a registration files, in words.
function returns(r: Registration): string {
  return r.return_frequency === "monthly" ? "Monthly returns" : `Quarterly returns, quarters from ${quarterMonths[r.quarter_start_month] ?? "January"}`;
}

type Draft = { registered_from: string; deregistered_from: string; gst_number: string; return_frequency: string; quarter_start_month: string };

function draftOf(r: Registration | null): Draft {
  return {
    registered_from: r?.registered_from ?? "",
    deregistered_from: r?.deregistered_from ?? "",
    gst_number: r?.gst_number ?? "",
    return_frequency: r?.return_frequency ?? "quarterly",
    quarter_start_month: String(r?.quarter_start_month ?? 1),
  };
}

// RegistrationForm records a registration, or changes one: only the fields changed are sent, over its version.
function RegistrationForm({ open, registration, onClose, onDone }: { open: boolean; registration: Registration | null; onClose: () => void; onDone: (what: string) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const [draft, setDraft] = useState<Draft | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const d = draft ?? draftOf(registration);
  const set = (k: keyof Draft, v: string) => setDraft({ ...d, [k]: v });
  const close = () => {
    setDraft(null);
    setProblem(null);
    onClose();
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    let out;
    if (registration) {
      const was = draftOf(registration);
      const changes: components["schemas"]["GSTRegistrationChanges"] = {};
      if (d.registered_from !== was.registered_from) changes.registered_from = d.registered_from;
      if (d.deregistered_from !== was.deregistered_from) changes.deregistered_from = d.deregistered_from; // "" clears it
      if (d.gst_number.trim() !== was.gst_number) changes.gst_number = d.gst_number.trim();
      if (d.return_frequency !== was.return_frequency) changes.return_frequency = d.return_frequency;
      if (d.quarter_start_month !== was.quarter_start_month) changes.quarter_start_month = Number(d.quarter_start_month);
      if (Object.keys(changes).length === 0) {
        setBusy(false);
        close();
        return;
      }
      out = await guarded(confirm, () =>
        api.PATCH("/v1/organisation/gst-registrations/{registration_id}", {
          params: { path: { registration_id: registration.id }, header: { "If-Match": `"${registration.version}"` } },
          body: changes,
        }),
      );
    } else {
      const key = newKey();
      out = await guarded(confirm, () =>
        api.POST("/v1/organisation/gst-registrations", {
          params: { header: { "Idempotency-Key": key } },
          body: {
            registered_from: d.registered_from,
            gst_number: d.gst_number.trim(),
            return_frequency: d.return_frequency,
            quarter_start_month: Number(d.quarter_start_month),
            ...(d.deregistered_from ? { deregistered_from: d.deregistered_from } : {}),
          },
        }),
      );
    }
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    await Promise.all(["gst-registrations", "gst-status"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
    onDone(registration ? `The GST registration ${out.data.gst_number} is changed.` : `The GST registration ${out.data.gst_number} is recorded.`);
    close();
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      eyebrow="Organisation settings"
      title={registration ? "Change the GST registration" : "Record a GST registration"}
      description="As IRAS's notice of registration gives it. Invoices dated from the first day are tax invoices."
    >
      {problem && <Refusal problem={problem} title="The GST registration was not saved" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-form fy-form--2">
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-number`}>
              GST registration number
            </label>
            <input className="fy-input" id={`${id}-number`} value={d.gst_number} maxLength={20} onChange={(e) => set("gst_number", e.target.value)} autoComplete="off" />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-from`}>
              Registered from
            </label>
            <input className="fy-input" id={`${id}-from`} type="date" value={d.registered_from} onChange={(e) => set("registered_from", e.target.value)} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-frequency`}>
              Returns
            </label>
            <select className="fy-select" id={`${id}-frequency`} value={d.return_frequency} onChange={(e) => set("return_frequency", e.target.value)}>
              <option value="quarterly">Quarterly</option>
              <option value="monthly">Monthly</option>
            </select>
          </div>
          {d.return_frequency === "quarterly" && (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-quarters`}>
                Quarters start in
              </label>
              <select className="fy-select" id={`${id}-quarters`} value={d.quarter_start_month} onChange={(e) => set("quarter_start_month", e.target.value)}>
                {[1, 2, 3].map((m) => (
                  <option key={m} value={String(m)}>
                    {quarterMonths[m]}
                  </option>
                ))}
              </select>
            </div>
          )}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-to`}>
              Deregistered from (optional)
            </label>
            <input
              className="fy-input"
              id={`${id}-to`}
              type="date"
              value={d.deregistered_from}
              onChange={(e) => set("deregistered_from", e.target.value)}
              aria-describedby={`${id}-to-help`}
            />
            <span className="fy-help" id={`${id}-to-help`}>
              The first day the company is no longer registered. Leave it empty while the registration lasts.
            </span>
          </div>
        </div>
        <p className="fy-small fy-secondary">Fyniq accounts for GST on the invoice basis.</p>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={close} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy}>
            {registration ? "Save the change" : "Record the registration"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// GSTRegistrations is the GST registration section of Settings › Organisation (FR-ORG-03): whether the company is
// registered today and for which return period, and each registration with its effective dates; those who may change
// the profile record and change them.
export function GSTRegistrations({ mayChange, onDone }: { mayChange: boolean; onDone: (what: string) => void }) {
  const list = useQuery({ queryKey: ["gst-registrations"], queryFn: listRegistrations });
  const status = useQuery({ queryKey: ["gst-status"], queryFn: statusToday });
  const [editing, setEditing] = useState<Registration | null>(null);
  const [adding, setAdding] = useState(false);
  const record = helpText("gst.record");
  const change = helpText("gst.change");
  const s = status.data;

  return (
    <section className="fy-card" aria-labelledby="gst-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="gst-title">
          GST registration
        </h2>
        {mayChange && (
          <Button size="sm" variant="secondary" onClick={() => setAdding(true)} title={record.tooltip}>
            <Icon of={Plus} />
            {record.label}
          </Button>
        )}
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        {status.isError && <Refusal problem={problemOf(status.error)} title="The GST status could not be read" />}
        {s &&
          (s.registered && s.registration ? (
            <p className="fy-row fy-row--wrap gap-2">
              <span className="fy-chip fy-chip--good">
                <Icon of={CircleCheck} />
                Registered today
              </span>
              <span className="fy-small fy-secondary">
                {s.registration.gst_number}
                {s.return_period ? `, return period ${date(s.return_period.starts_on)} to ${date(s.return_period.ends_on)}` : ""}
              </span>
            </p>
          ) : (
            <p className="fy-row fy-row--wrap gap-2">
              <span className="fy-chip fy-chip--neutral">
                <Icon of={CircleMinus} />
                Not GST-registered today
              </span>
              <span className="fy-small fy-secondary">Invoices are not tax invoices and carry no GST.</span>
            </p>
          ))}
        {list.isError && <Refusal problem={problemOf(list.error)} title="The GST registrations could not be shown" />}
        {list.data && list.data.length === 0 && (
          <p className="fy-small fy-secondary">No GST registration is recorded. Once IRAS registers the company, record the date and number on its notice.</p>
        )}
      </div>
      {list.data && list.data.length > 0 && (
        <ul className="fy-list" aria-labelledby="gst-title">
          {list.data.map((r) => (
            <li key={r.id} className="fy-list__item items-start">
              <span className="fy-list__main">
                <span className="fy-list__title fy-num">{r.gst_number}</span>
                <span className="fy-list__meta whitespace-normal">
                  From {date(r.registered_from)}
                  {r.deregistered_from ? `, deregistered from ${date(r.deregistered_from)}` : ""}
                </span>
                <span className="fy-list__meta whitespace-normal">{returns(r)}</span>
              </span>
              {mayChange && (
                <Button size="sm" onClick={() => setEditing(r)} aria-label={`${change.label}: ${r.gst_number}`} title={change.tooltip}>
                  <Icon of={Pencil} />
                  <span className="fy-from-medium">{change.label}</span>
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      <RegistrationForm open={adding} registration={null} onClose={() => setAdding(false)} onDone={onDone} />
      <RegistrationForm key={editing?.id ?? "none"} open={editing !== null} registration={editing} onClose={() => setEditing(null)} onDone={onDone} />
    </section>
  );
}
