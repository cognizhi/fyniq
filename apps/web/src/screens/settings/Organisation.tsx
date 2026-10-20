import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, Hourglass, Lock, Pencil, RotateCcw, ShieldCheck, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date, timeZone } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import type { Approval } from "../approvals/words";
import { SettingsPage } from "./SettingsPage";

type Setting = components["schemas"]["Setting"];
type Profile = components["schemas"]["OrganisationProfile"];

// valueText writes a setting's value as people read it, by its kind; money stays a decimal string.
export function valueText(kind: Setting["kind"], v: string): string {
  switch (kind) {
    case "switch":
      return v === "true" ? "On" : "Off";
    case "days":
      return `${v} ${v === "1" ? "day" : "days"}`;
    case "years":
      return `${v} ${v === "1" ? "year" : "years"}`;
    case "amount_sgd":
      return `S$${formatAmount(v)}`;
    case "percent":
      return `${v}%`;
    default:
      return v;
  }
}

// today is the organisation's date today, YYYY-MM-DD (a change takes effect today or later).
function today(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
}

async function listSettings() {
  const { data, error } = await api.GET("/v1/organisation/settings", { params: { query: { page_size: 200 } } });
  if (error) throw error;
  return data.items ?? [];
}

async function getProfile() {
  const r = await api.GET("/v1/organisation/profile");
  if (r.error) throw r.error;
  return { profile: r.data, etag: etagOf(r.response) || `"${r.data.version}"` };
}

function ValueInput({ setting: s, value, onChange, id, describedBy, invalid }: { setting: Setting; value: string; onChange: (v: string) => void; id: string; describedBy: string; invalid: boolean }) {
  if (s.kind === "switch")
    return (
      <div className="fy-row gap-4" role="radiogroup" aria-labelledby={`${id}-label`} aria-describedby={describedBy}>
        {(["true", "false"] as const).map((v) => (
          <label key={v} className="fy-row gap-2">
            <input type="radio" name={id} value={v} checked={value === v} onChange={() => onChange(v)} />
            {v === "true" ? "On" : "Off"}
          </label>
        ))}
      </div>
    );
  const numeric = s.kind !== "text";
  return (
    <input
      className="fy-input max-w-72"
      id={id}
      value={value}
      inputMode={s.kind === "days" || s.kind === "years" ? "numeric" : numeric ? "decimal" : undefined}
      onChange={(e) => onChange(e.target.value)}
      aria-invalid={invalid || undefined}
      aria-describedby={describedBy}
    />
  );
}

// check finds what the page can tell before asking the API: the shape of the value for its kind.
function check(s: Setting, v: string): string {
  const t = v.trim();
  if (s.kind === "days" || s.kind === "years") return /^\d{1,6}$/.test(t) ? "" : `Enter a whole number of ${s.kind}.`;
  if (s.kind === "amount_sgd") return /^\d{1,14}(\.\d{1,2})?$/.test(t) ? "" : "Enter an amount in Singapore dollars, such as 1000.00, without the S$ or commas.";
  if (s.kind === "percent") return /^\d{1,3}(\.\d{1,4})?$/.test(t) ? "" : "Enter a percentage, such as 3, without the % sign.";
  if (s.kind === "text") return t ? "" : "Enter a value.";
  return "";
}

function ChangeSetting({ setting, onClose, onDone }: { setting: Setting | null; onClose: () => void; onDone: (what: string, waits?: Approval) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const one = useQuery({
    queryKey: ["setting", setting?.key],
    enabled: setting !== null,
    queryFn: async () => {
      const r = await api.GET("/v1/organisation/settings/{key}", { params: { path: { key: setting?.key ?? "" } } });
      if (r.error) throw r.error;
      return { setting: r.data, etag: etagOf(r.response) || `"${r.data.version}"` };
    },
  });
  const [value, setValue] = useState<string | null>(null);
  const [from, setFrom] = useState(today());
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [error, setError] = useState("");
  const [previewed, setPreviewed] = useState("");
  const s = one.data?.setting ?? setting;
  const v = value ?? s?.value ?? "";

  const close = () => {
    setValue(null);
    setFrom(today());
    setProblem(null);
    setError("");
    setPreviewed("");
    onClose();
  };

  const send = async (preview: boolean, reset: boolean) => {
    if (!s || !one.data) return;
    const err = reset ? "" : check(s, v);
    setError(err);
    setPreviewed("");
    if (err) return;
    setBusy(true);
    setProblem(null);
    const etag = one.data.etag;
    const body = reset ? { reset: true, effective_from: from } : { value: v.trim(), effective_from: from };
    const header = { "If-Match": etag, ...(preview ? { Prefer: "return=preview" } : {}) };
    const out = await guarded(confirm, () => api.PATCH("/v1/organisation/settings/{key}", { params: { path: { key: s.key }, header }, body }));
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    if (preview) {
      setPreviewed(`Fyniq checked it: ${reset ? "the recommended value" : valueText(s.kind, v.trim())} from ${date(from)} passes every rule. Nothing is saved yet.`);
      return;
    }
    await Promise.all(["settings", "setting", "approvals", "shell"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
    if (out.response.status === 202) onDone("", out.data as unknown as Approval);
    else onDone(`${s.label}: ${reset ? "reset to recommended" : valueText(s.kind, v.trim())} from ${date(from)}.`);
    close();
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    void send(false, false);
  };
  const reset = helpText("setting.reset");

  return (
    <Dialog
      open={setting !== null}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      wide
      eyebrow="Organisation settings"
      title={s ? `Change: ${s.label}` : "Change the setting"}
      description={s ? `${s.reason.replace(/[.\s]*$/, "")}. Recommended: ${valueText(s.kind, s.recommended_value)}.` : undefined}
    >
      {one.isError && <Refusal problem={problemOf(one.error)} title="The setting could not be read" />}
      {problem && <Refusal problem={problem} title="The setting was not changed" />}
      {s && one.data && (
        <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
          {s.needs_second_approver && (
            <div className="fy-banner fy-banner--info" role="note">
              <Icon of={ShieldCheck} size={20} />
              <div className="fy-banner__text">
                <span className="fy-small">This setting needs a second approver: saving asks for approval, and it keeps its value until another Owner approves.</span>
              </div>
            </div>
          )}
          <div className="fy-form fy-form--2">
            <div className="fy-field">
              <span className="fy-label" id={`${id}-label`}>
                {s.kind === "switch" ? s.label : <label htmlFor={id}>New value</label>}
              </span>
              <ValueInput setting={s} value={v} onChange={(x) => setValue(x)} id={id} describedBy={`${id}-help${error ? ` ${id}-error` : ""}`} invalid={!!error} />
              <span className="fy-help" id={`${id}-help`}>
                Now {valueText(s.kind, s.value)}
                {s.min ? `; at least ${valueText(s.kind, s.min)}` : ""}
                {s.max ? `; at most ${valueText(s.kind, s.max)}` : ""}
                {s.lengthen_only ? "; it can only be made longer" : ""}.
              </span>
              {error && (
                <span className="fy-error" id={`${id}-error`}>
                  {error}
                </span>
              )}
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-from`}>
                Takes effect from
              </label>
              <input className="fy-input" id={`${id}-from`} type="date" min={today()} value={from} onChange={(e) => setFrom(e.target.value)} aria-describedby={`${id}-from-help`} />
              <span className="fy-help" id={`${id}-from-help`}>
                Today or later, never earlier. Earlier values are kept with their dates.
              </span>
            </div>
          </div>
          {previewed && (
            <p className="fy-small fy-row gap-2" role="status">
              <Icon of={CircleCheck} />
              {previewed}
            </p>
          )}
          <div className="fy-row fy-row--wrap justify-between gap-2">
            <Button type="button" variant="ghost" disabled={busy || s.status === "recommended"} onClick={() => void send(false, true)} title={reset.tooltip}>
              <Icon of={RotateCcw} />
              {reset.label}
            </Button>
            <span className="fy-row gap-2">
              <Button type="button" disabled={busy} onClick={() => void send(true, false)}>
                Preview
              </Button>
              <Button variant="primary" type="submit" disabled={busy}>
                {s.needs_second_approver ? "Ask for approval" : "Save"}
              </Button>
            </span>
          </div>
          {(one.data.setting.changes ?? []).length > 0 && (
            <section className="fy-stack fy-stack--2" aria-labelledby={`${id}-history`}>
              <h3 className="fy-h3" id={`${id}-history`}>
                Earlier values
              </h3>
              <ul className="fy-list rounded-md border border-border" aria-labelledby={`${id}-history`}>
                {(one.data.setting.changes ?? []).map((c) => (
                  <li key={c.version} className="fy-list__item">
                    <span className="fy-list__main">
                      <span className="fy-list__title">{valueText(s.kind, c.value)}</span>
                      <span className="fy-list__meta">
                        From {date(c.effective_from)} · recorded {date(c.recorded_at)}
                      </span>
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          )}
        </form>
      )}
    </Dialog>
  );
}

const profileFields: { key: keyof Profile; label: string; kind?: "date" | "month" | "long" }[] = [
  { key: "legal_name", label: "Legal name" },
  { key: "uen", label: "UEN" },
  { key: "registered_address", label: "Registered office address", kind: "long" },
  { key: "mailing_address", label: "Mailing address", kind: "long" },
  { key: "email", label: "Email on documents" },
  { key: "phone", label: "Telephone" },
  { key: "website", label: "Website" },
  { key: "bank_name", label: "Bank" },
  { key: "bank_account_name", label: "Bank account name" },
  { key: "bank_account_number", label: "Bank account number" },
  { key: "paynow_uen", label: "PayNow UEN" },
  { key: "document_footer", label: "Document footer", kind: "long" },
  { key: "fy_start_month", label: "Financial year starts in", kind: "month" },
  { key: "books_start", label: "Books start", kind: "date" },
  { key: "first_year_end", label: "First financial year ends", kind: "date" },
  { key: "time_zone", label: "Time zone" },
];

const months = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];

function shown(f: (typeof profileFields)[number], p: Profile): string {
  const v = p[f.key];
  if (v === undefined || v === "") return "Not set";
  if (f.kind === "month") return months[Number(v) - 1] ?? String(v);
  if (f.kind === "date") return date(String(v));
  return String(v);
}

function EditProfile({ open, data, onClose, onDone }: { open: boolean; data: { profile: Profile; etag: string } | undefined; onClose: () => void; onDone: () => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const [draft, setDraft] = useState<Partial<Record<keyof Profile, string>>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const close = () => {
    setDraft({});
    setProblem(null);
    onClose();
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!data) return;
    const changes: Record<string, string | number> = {};
    for (const f of profileFields) {
      const d = draft[f.key];
      if (d === undefined || d === String(data.profile[f.key] ?? "")) continue;
      changes[f.key] = f.kind === "month" ? Number(d) : d.trim();
    }
    if (Object.keys(changes).length === 0) {
      close();
      return;
    }
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => api.PATCH("/v1/organisation/profile", { params: { header: { "If-Match": data.etag } }, body: changes }));
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    await Promise.all(["profile", "shell"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
    onDone();
    close();
  };
  return (
    <Dialog open={open} onOpenChange={(o) => !o && close()} dismissible={!busy} wide eyebrow="Organisation settings" title="Edit the profile" description="What documents print about the company, and how its financial years run. A field left empty is cleared.">
      {problem && <Refusal problem={problem} title="The profile was not saved" />}
      {data && (
        <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
          <div className="fy-form fy-form--2">
            {profileFields.map((f) => {
              const fid = `${id}-${f.key}`;
              const v = draft[f.key] ?? String(data.profile[f.key] ?? "");
              const set = (x: string) => setDraft({ ...draft, [f.key]: x });
              return (
                <div key={f.key} className={f.kind === "long" ? "fy-field fy-field--full" : "fy-field"}>
                  <label className="fy-label" htmlFor={fid}>
                    {f.label}
                  </label>
                  {f.kind === "long" ? (
                    <textarea className="fy-textarea" id={fid} value={v} onChange={(e) => set(e.target.value)} />
                  ) : f.kind === "month" ? (
                    <select className="fy-select" id={fid} value={v} onChange={(e) => set(e.target.value)}>
                      {months.map((m, i) => (
                        <option key={m} value={String(i + 1)}>
                          {m}
                        </option>
                      ))}
                    </select>
                  ) : (
                    <input className="fy-input" id={fid} type={f.kind === "date" ? "date" : "text"} value={v} onChange={(e) => set(e.target.value)} />
                  )}
                </div>
              );
            })}
          </div>
          <div className="fy-row justify-end gap-2">
            <Button variant="ghost" type="button" onClick={close} disabled={busy}>
              Cancel
            </Button>
            <Button variant="primary" type="submit" disabled={busy}>
              Save the profile
            </Button>
          </div>
        </form>
      )}
    </Dialog>
  );
}

// Organisation is Settings › Organisation (docs/design/screens OrganisationSettings; FR-ORG-01,
// FR-ORG-04…08): the profile documents print, and every setting with its recommended value, why, and
// whether it needs a second approver. A change takes effect from a day, today or later; a preview
// checks it without saving (FR-INT-14); one under dual control waits for another Owner (FM-1).
export function Organisation() {
  const { session } = useSignedIn();
  const settings = useQuery({ queryKey: ["settings"], queryFn: listSettings });
  const profile = useQuery({ queryKey: ["profile"], queryFn: getProfile });
  const [editing, setEditing] = useState<Setting | null>(null);
  const [editProfile, setEditProfile] = useState(false);
  const [status, setStatus] = useState("");
  const [waits, setWaits] = useState<Approval | null>(null);
  const change = helpText("setting.change");
  const mayChange = (session.roles ?? []).some((r) => ["owner", "administrator", "controller"].includes(r));
  const groups = [...new Set((settings.data ?? []).map((s) => s.group))];

  return (
    <SettingsPage
      title="Organisation" screen="organisation-settings"
      lead="The profile documents print, and the choices that apply to the whole company."
      actions={
        mayChange && (
          <Button onClick={() => setEditProfile(true)}>
            <Icon of={Pencil} />
            Edit the profile
          </Button>
        )
      }
    >
      <p className="fy-small" role="status">
        {status}
      </p>
      {waits && (
        <div className="fy-banner fy-banner--info" role="status">
          <Icon of={Hourglass} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Waiting for approval</span>
            <span className="fy-small">
              {waits.summary}. The setting keeps its value until it is approved.{" "}
              <Link to="/approvals/$approvalId" params={{ approvalId: waits.id }}>
                View the request
              </Link>
            </span>
          </div>
        </div>
      )}
      <section className="fy-card" aria-labelledby="profile-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="profile-title">
            Profile
          </h2>
        </div>
        {profile.isError && <Refusal problem={problemOf(profile.error)} title="The profile could not be shown" />}
        {profile.data && (
          <dl className="fy-dl fy-dl--3 fy-card__body">
            {profileFields.map((f) => (
              <div key={f.key}>
                <dt>{f.label}</dt>
                <dd className={profile.data.profile[f.key] ? "" : "fy-muted"}>{shown(f, profile.data.profile)}</dd>
              </div>
            ))}
          </dl>
        )}
      </section>
      {settings.isError && <Refusal problem={problemOf(settings.error)} title="The settings could not be shown" />}
      {settings.isPending && <p className="fy-secondary">Loading the settings…</p>}
      {groups.map((g) => {
        const gid = `group-${g.replace(/\W+/g, "-").toLowerCase()}`;
        return (
          <section key={g} className="fy-card" aria-labelledby={gid}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={gid}>
                {g}
              </h2>
            </div>
            <ul className="fy-list" aria-labelledby={gid}>
              {(settings.data ?? [])
                .filter((s) => s.group === g)
                .map((s) => (
                  <li key={s.key} className="fy-list__item items-start">
                    <span className="fy-list__main">
                      <span className="fy-list__title whitespace-normal">
                        {s.label}: <span className="fy-num">{valueText(s.kind, s.value)}</span>
                      </span>
                      <span className="fy-list__meta whitespace-normal">{s.reason}</span>
                      {(s.scheduled ?? []).map((c) => (
                        <span key={c.version} className="fy-list__meta whitespace-normal">
                          From {date(c.effective_from)}: {valueText(s.kind, c.value)}
                        </span>
                      ))}
                      <span className="fy-row fy-row--wrap gap-1">
                        {s.status === "recommended" ? (
                          <span className="fy-chip fy-chip--good">
                            <Icon of={CircleCheck} />
                            Recommended
                          </span>
                        ) : (
                          <span className="fy-chip fy-chip--warning">
                            <Icon of={TriangleAlert} />
                            Changed from recommended
                          </span>
                        )}
                        {s.needs_second_approver && <span className="fy-tag">Needs a second approver</span>}
                      </span>
                    </span>
                    {s.set_by_platform ? (
                      <span className="fy-chip fy-chip--neutral" title="The platform sets it; it never changes here.">
                        <Icon of={Lock} />
                        Set by the platform
                      </span>
                    ) : (
                      mayChange && (
                        <Button size="sm" onClick={() => setEditing(s)} aria-label={`${change.label}: ${s.label}`} title={change.tooltip}>
                          <Icon of={Pencil} />
                          <span className="fy-from-medium">{change.label}</span>
                        </Button>
                      )
                    )}
                  </li>
                ))}
            </ul>
          </section>
        );
      })}
      <ChangeSetting
        setting={editing}
        onClose={() => setEditing(null)}
        onDone={(what, w) => {
          setStatus(what);
          setWaits(w ?? null);
        }}
      />
      <EditProfile open={editProfile} data={profile.data} onClose={() => setEditProfile(false)} onDone={() => setStatus("The profile is saved.")} />
    </SettingsPage>
  );
}
