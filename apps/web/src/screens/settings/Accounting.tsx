import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { BookCheck, CircleCheck, Hourglass, PenLine, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { who } from "../accounting/Journals";
import { SettingsPage } from "./SettingsPage";

type Year = components["schemas"]["AccountingYear"];
type Area = components["schemas"]["AccountingPolicyArea"];
type Framework = components["schemas"]["AccountingFramework"]["framework"];

// The roles that record the framework and the policies (Appendix D, rev 0.62); the API still decides.
const writers = ["owner", "controller", "accountant"];

const frameworks: { code: Framework; name: string }[] = [
  { code: "sfrs_se", name: "SFRS for Small Entities" },
  { code: "frs", name: "FRS" },
  { code: "sfrs_i", name: "SFRS(I)" },
];

// dayAfter is the day after a date, as YYYY-MM-DD.
export function dayAfter(d: string): string {
  const t = new Date(`${d}T00:00:00Z`);
  t.setUTCDate(t.getUTCDate() + 1);
  return t.toISOString().slice(0, 10);
}

async function readYear(on?: string): Promise<Year> {
  const { data, error } = await api.GET("/v1/organisation/accounting", { params: { query: on ? { on } : {} } });
  if (error) throw error;
  return data;
}

// Editing is what the dialog records: the framework, or one area's policy, from the year shown.
type Editing = { kind: "framework" } | { kind: "policy"; area: Area };

function Editor({ year, editing, onClose, onDone }: { year: Year; editing: Editing | null; onClose: () => void; onDone: (said: string) => void }) {
  const id = useId();
  const queries = useQueryClient();
  const area = editing?.kind === "policy" ? editing.area : null;
  const [framework, setFramework] = useState<Framework>(year.framework?.framework ?? (year.recommended_framework as Framework));
  const [edition, setEdition] = useState(year.framework?.edition ?? year.recommended_edition);
  const [wording, setWording] = useState(area?.policy?.wording ?? "");
  const [error, setError] = useState("");
  const [checked, setChecked] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);

  const close = () => {
    setError("");
    setChecked("");
    setProblem(null);
    onClose();
  };
  const send = async (preview: boolean) => {
    const text = area ? wording.trim() : edition.trim();
    const err = text === "" ? (area ? "Write the policy, or say that it does not apply." : "Name the edition.") : "";
    setError(err);
    setChecked("");
    if (err) return;
    setBusy(true);
    setProblem(null);
    const header = { "Idempotency-Key": newKey(), ...(preview ? { Prefer: "return=preview" } : {}) };
    const out = area
      ? await api.POST("/v1/organisation/accounting/policies/{area}", { params: { path: { area: area.area }, header }, body: { wording: text, from_on: year.starts_on } })
      : await api.POST("/v1/organisation/accounting/framework", { params: { header }, body: { framework, edition: text, from_on: year.starts_on } });
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    if (preview) {
      setChecked(`Fyniq checked it: it can be recorded from ${date(year.starts_on)}. Nothing was saved.`);
      return;
    }
    await queries.invalidateQueries({ queryKey: ["accounting"] });
    onDone(
      area
        ? `${area.title}: version ${out.data.version} recorded from ${date(year.starts_on)}. It waits for the accountant's approval.`
        : `The framework: version ${out.data.version} recorded from ${date(year.starts_on)}. It waits for the accountant's confirmation.`,
    );
    close();
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    void send(false);
  };
  const record = helpText(area ? "accounting.record-policy" : "accounting.record-framework");

  return (
    <Dialog
      open={editing !== null}
      onOpenChange={(o) => !o && close()}
      dismissible={!busy}
      wide
      eyebrow="Accounting framework and policies"
      title={area ? `Record the policy: ${area.title}` : "Record the framework"}
      description={`It applies from ${date(year.starts_on)} until a later version replaces it. A version never changes: a correction is a new version, which the accountant signs again.`}
    >
      {problem && <Refusal problem={problem} title="Nothing was recorded" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        {area ? (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-wording`}>
              The policy
            </label>
            <textarea
              className="fy-textarea"
              id={`${id}-wording`}
              rows={6}
              maxLength={4000}
              value={wording}
              onChange={(e) => setWording(e.target.value)}
              aria-invalid={!!error}
              aria-describedby={`${id}-wording-help${error ? ` ${id}-error` : ""}`}
            />
            <span className="fy-help" id={`${id}-wording-help`}>
              {area.decides} Write it as the accountant decided it; Fyniq never writes it for you.
              {area.setting && " The amount itself is a setting under Settings › Organisation › Defaults."}
            </span>
            {error && (
              <span className="fy-error" id={`${id}-error`}>
                {error}
              </span>
            )}
          </div>
        ) : (
          <div className="fy-form fy-form--2">
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-framework`}>
                Framework
              </label>
              <select
                className="fy-select"
                id={`${id}-framework`}
                value={framework}
                onChange={(e) => {
                  const f = e.target.value as Framework;
                  setFramework(f);
                  if (f === "sfrs_se") setEdition(year.recommended_edition);
                }}
              >
                {frameworks.map((f) => (
                  <option key={f.code} value={f.code}>
                    {f.name}
                    {f.code === year.recommended_framework ? " (recommended)" : ""}
                  </option>
                ))}
              </select>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-edition`}>
                Edition
              </label>
              <input
                className="fy-input"
                id={`${id}-edition`}
                maxLength={120}
                value={edition}
                onChange={(e) => setEdition(e.target.value)}
                aria-invalid={!!error}
                aria-describedby={`${id}-edition-help${error ? ` ${id}-error` : ""}`}
              />
              <span className="fy-help" id={`${id}-edition-help`}>
                For SFRS for Small Entities this year: {year.recommended_edition}.
              </span>
              {error && (
                <span className="fy-error" id={`${id}-error`}>
                  {error}
                </span>
              )}
            </div>
          </div>
        )}
        {checked && (
          <p className="fy-small fy-row gap-2" role="status">
            <Icon of={CircleCheck} />
            {checked}
          </p>
        )}
        <div className="fy-row fy-row--wrap justify-end gap-2">
          <Button type="button" variant="ghost" onClick={close} disabled={busy}>
            Cancel
          </Button>
          <Button type="button" disabled={busy} onClick={() => void send(true)}>
            Check
          </Button>
          <Button variant="primary" type="submit" disabled={busy} title={record.tooltip}>
            {record.label}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// Signed shows who signed a version and when, or that it waits.
function Signed({ by, at, waiting }: { by?: components["schemas"]["Person"]; at?: string; waiting: string }) {
  return by && at ? (
    <span className="fy-chip fy-chip--good">
      <Icon of={CircleCheck} />
      {who(by)}, {date(at)}
    </span>
  ) : (
    <span className="fy-chip fy-chip--warning">
      <Icon of={Hourglass} />
      {waiting}
    </span>
  );
}

// Accounting is Settings › Accounting framework and policies (docs/design/screens OrganisationSettings; FR-ORG-10,
// G4-017; PRD-001 rev 0.62): the framework and edition, and each accounting policy, in force for a financial year. The
// Owner, the Controller or the Accountant records them; a member who holds the Accountant role confirms or approves each
// version, never its writer. Until everything in force is signed, the year's statements are management statements.
export function Accounting() {
  const { session } = useSignedIn();
  const queries = useQueryClient();
  const [next, setNext] = useState(false);
  const current = useQuery({ queryKey: ["accounting", "current"], queryFn: () => readYear() });
  const later = useQuery({
    queryKey: ["accounting", "next", current.data?.ends_on],
    enabled: next && !!current.data,
    queryFn: () => readYear(dayAfter(current.data?.ends_on ?? "")),
  });
  const shown = next ? later : current;
  const year = shown.data;
  const [editing, setEditing] = useState<Editing | null>(null);
  const [done, setDone] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const may = (session.roles ?? []).some((r) => writers.includes(r));
  const confirm = helpText("accounting.confirm-framework");
  const approve = helpText("accounting.approve-policy");

  const sign = async (what: { framework: number } | { area: Area; version: number }) => {
    setBusy(true);
    setProblem(null);
    setDone("");
    const header = { "Idempotency-Key": newKey() };
    const out =
      "framework" in what
        ? await api.POST("/v1/organisation/accounting/framework/{version}/confirm", { params: { path: { version: what.framework }, header } })
        : await api.POST("/v1/organisation/accounting/policies/{area}/{version}/approve", { params: { path: { area: what.area.area, version: what.version }, header } });
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    await queries.invalidateQueries({ queryKey: ["accounting"] });
    setDone("framework" in what ? "The framework is confirmed." : `${what.area.title}: approved.`);
  };

  return (
    <SettingsPage
      title="Accounting framework and policies"
      screen="organisation-settings"
      lead="The framework your statements are prepared under, and the accounting policies Fyniq follows. Anyone who keeps them records them; a member with the Accountant role confirms and approves them."
    >
      <div className="fy-segmented self-start" role="group" aria-label="Financial year">
        <button type="button" aria-pressed={!next} onClick={() => setNext(false)}>
          This financial year
        </button>
        <button type="button" aria-pressed={next} onClick={() => setNext(true)} disabled={!current.data}>
          Next financial year
        </button>
      </div>
      {shown.isError && <Refusal problem={problemOf(shown.error)} title="The framework and policies could not be read" />}
      {shown.isPending && !shown.isError && <p className="fy-secondary">Loading the framework and policies…</p>}
      {problem && <Refusal problem={problem} title="Nothing was signed" />}
      {done && (
        <p className="fy-small fy-row gap-2" role="status">
          <Icon of={CircleCheck} />
          {done}
        </p>
      )}
      {year && (
        <>
          <h2 className="fy-h2">
            Financial year {date(year.starts_on)} to {date(year.ends_on)}
          </h2>
          {year.status === "approved" ? (
            <div className="fy-banner fy-banner--good" role="note">
              <Icon of={BookCheck} size={20} />
              <div className="fy-banner__text">
                <span className="fy-strong">Statements: {year.statement_title}</span>
                <span className="fy-small">{year.statement_basis}. The accountant has confirmed the framework and approved every policy.</span>
              </div>
            </div>
          ) : (
            <div className="fy-banner fy-banner--warning" role="note">
              <Icon of={TriangleAlert} size={20} />
              <div className="fy-banner__text">
                <span className="fy-strong">Statements: {year.statement_title}</span>
                <span className="fy-small">
                  They name no framework until the accountant confirms the framework and approves every policy: {year.checks.length} {year.checks.length === 1 ? "thing" : "things"} to do.
                </span>
              </div>
            </div>
          )}

          <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby="framework-title">
            <div className="fy-row fy-row--wrap justify-between gap-2">
              <h3 className="fy-h3" id="framework-title">
                Framework
              </h3>
              {may && (
                <Button onClick={() => setEditing({ kind: "framework" })}>
                  <Icon of={PenLine} />
                  {year.framework ? "Record a new version" : helpText("accounting.record-framework").label}
                </Button>
              )}
            </div>
            {year.framework ? (
              <>
                <p className="fy-strong">
                  {year.framework.name}, {year.framework.edition}
                </p>
                <p className="fy-small fy-secondary">
                  Version {year.framework.version}, from {date(year.framework.from_on)}; recorded by {who(year.framework.written_by)} on {date(year.framework.written_at)}.
                </p>
                <div className="fy-row fy-row--wrap gap-2">
                  <Signed by={year.framework.confirmed_by} at={year.framework.confirmed_at} waiting="Not confirmed" />
                  {year.framework.you_may_confirm && (
                    <Button variant="primary" disabled={busy} title={confirm.tooltip} onClick={() => void sign({ framework: year.framework?.version ?? 0 })}>
                      {confirm.label}
                    </Button>
                  )}
                </div>
              </>
            ) : (
              <p className="fy-secondary">
                None recorded. Fyniq recommends SFRS for Small Entities, {year.recommended_edition}, if the accountant confirms the company qualifies.
              </p>
            )}
            {year.checks
              .filter((c) => !c.area)
              .map((c) => (
                <p key={c.code} className="fy-small fy-secondary">
                  {c.detail} {c.fix}.
                </p>
              ))}
          </section>

          <section className="fy-card" aria-labelledby="policies-title">
            <div className="fy-card__head">
              <h3 className="fy-h3" id="policies-title">
                Accounting policies
              </h3>
              <span className="fy-badge fy-badge--quiet">
                {year.policies.filter((p) => p.policy?.approved_by).length} of {year.policies.length} approved
              </span>
            </div>
            <ul className="fy-list" aria-labelledby="policies-title">
              {year.policies.map((a) => (
                <li key={a.area} className="fy-list__item items-start">
                  <span className="fy-list__main">
                    <span className="fy-list__title whitespace-normal">{a.title}</span>
                    <span className="fy-list__meta whitespace-normal">{a.decides}</span>
                    {a.policy && (
                      <>
                        <span className="fy-small whitespace-pre-line">{a.policy.wording}</span>
                        <span className="fy-list__meta whitespace-normal">
                          Version {a.policy.version}, from {date(a.policy.from_on)}; written by {who(a.policy.written_by)} on {date(a.policy.written_at)}.
                        </span>
                      </>
                    )}
                    {a.setting && (
                      <span className="fy-small">
                        <Link to="/settings/organisation">The amount is under Settings › Organisation › Defaults</Link>
                      </span>
                    )}
                    <span className="fy-row fy-row--wrap gap-2 pt-1">
                      {a.policy ? (
                        <Signed by={a.policy.approved_by} at={a.policy.approved_at} waiting="Not approved" />
                      ) : (
                        <span className="fy-chip fy-chip--neutral">
                          <Icon of={PenLine} />
                          Not recorded
                        </span>
                      )}
                      {a.policy?.you_may_approve && (
                        <Button variant="primary" disabled={busy} title={approve.tooltip} aria-label={`${approve.label}: ${a.title}`} onClick={() => void sign({ area: a, version: a.policy?.version ?? 0 })}>
                          {approve.label}
                        </Button>
                      )}
                      {may && (
                        <Button variant="ghost" aria-label={`${a.policy ? "Change" : "Write"}: ${a.title}`} onClick={() => setEditing({ kind: "policy", area: a })}>
                          <Icon of={PenLine} />
                          {a.policy ? "Change" : "Write"}
                        </Button>
                      )}
                    </span>
                  </span>
                </li>
              ))}
            </ul>
          </section>
          {editing && <Editor key={editing.kind === "policy" ? editing.area.area : "framework"} year={year} editing={editing} onClose={() => setEditing(null)} onDone={setDone} />}
        </>
      )}
    </SettingsPage>
  );
}
