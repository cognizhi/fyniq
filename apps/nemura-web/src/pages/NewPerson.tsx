import { useEffect, useState, type FormEvent } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { t } from "../i18n";
import { navigate } from "../route";
import { Shell, admin, hr, team } from "./Shell";

type Entry = components["schemas"]["DirectoryEntry"];

// NewPerson adds a person with their first job from their start date (NM-PPL-01). The ID is made here, so a second send of the same form is refused by the API, not done twice.
export function NewPerson({ roles }: { roles: readonly string[] }) {
  const [id] = useState(() => crypto.randomUUID());
  const [people, setPeople] = useState<Entry[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    let live = true;
    api.GET("/v1/people").then(({ data }) => live && setPeople(data?.people ?? []), () => undefined);
    return () => {
      live = false;
    };
  }, []);
  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    const text = (k: string): string => String(f.get(k) ?? "").trim();
    setBusy(true);
    setError("");
    try {
      const { error: problem } = await api.POST("/v1/people", {
        body: {
          id,
          person: { legal_name: text("legal"), preferred_name: text("preferred"), work_email: text("email"), phone: text("phone"), employment_start: text("start") },
          job: { job_title: text("jobtitle"), department: text("department"), worksite: "SG", employment_type: text("type") as "full_time", manager_id: text("manager") || undefined },
        },
      });
      if (problem) setError((problem as { detail?: string }).detail ?? t("new.failed"));
      else navigate(`/people/${id}`);
    } catch {
      setError(t("new.failed"));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <h1 className="fy-h1">{t("new.title")}</h1>
        <p className="fy-secondary">{t("new.lead")}</p>
        {!hr(roles) && <p role="alert" className="fy-secondary">{t("features.none")}</p>}
        <form className="fy-stack fy-stack--3" onSubmit={(e) => void submit(e)}>
          <Field id="np-legal" name="legal" label="new.legal" required />
          <Field id="np-preferred" name="preferred" label="new.preferred" />
          <Field id="np-email" name="email" label="new.email" type="email" />
          <Field id="np-phone" name="phone" label="new.phone" type="tel" />
          <Field id="np-start" name="start" label="new.start" type="date" required />
          <Field id="np-title" name="jobtitle" label="new.jobtitle" required />
          <Field id="np-department" name="department" label="new.department" />
          <div className="fy-field">
            <label className="fy-label" htmlFor="np-type">{t("new.type")}</label>
            <select className="fy-input" id="np-type" name="type" defaultValue="full_time">
              {["full_time", "part_time", "contract", "intern"].map((v) => (
                <option key={v} value={v}>{t(`value.${v}`)}</option>
              ))}
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor="np-manager">{t("new.manager")}</label>
            <select className="fy-input" id="np-manager" name="manager" defaultValue="">
              <option value="">{t("new.nomanager")}</option>
              {people.map((p) => (
                <option key={p.id} value={p.id}>{p.name}</option>
              ))}
            </select>
          </div>
          {error && <p role="alert" className="fy-secondary">{error}</p>}
          <button className="fy-btn fy-btn--primary" type="submit" disabled={busy || !hr(roles)}>{busy ? t("new.saving") : t("new.submit")}</button>
        </form>
      </main>
    </Shell>
  );
}

function Field({ id, name, label, type = "text", required }: { id: string; name: string; label: string; type?: string; required?: boolean }) {
  return (
    <div className="fy-field">
      <label className="fy-label" htmlFor={id}>{t(label)}</label>
      <input className="fy-input" id={id} name={name} type={type} required={required} maxLength={200} />
    </div>
  );
}
