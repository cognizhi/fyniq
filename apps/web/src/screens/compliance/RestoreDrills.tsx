import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, CircleX, Plus } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Drill = components["schemas"]["RestoreDrillView"];
type Scenario = Drill["scenario"];

async function drills() {
  const { data, error } = await api.GET("/v1/compliance/restore-drills");
  if (error) throw error;
  return data.items;
}

// The scenarios a drill rehearses (NFR-DR-01, NFR-DR-02; docs/runbooks/restore-drill.md), in words.
const scenarios: Record<Scenario, string> = {
  project_loss: "Loss of the production project: the weekly export restored elsewhere",
  corruption: "Database corruption or a mistake: a copy at a point in time",
  zone_failure: "A zone's failure: the standby takes over",
};

// DR-06's checks on the restored copy, in the order the runbook makes them.
const checks = [
  ["ledger", "The ledger's verification passed"],
  ["audit_chain", "The audit chain's verification passed"],
  ["documents", "Documents and evidence opened"],
  ["keys", "The keys decrypted and signed"],
  ["sign_in", "People signed in"],
  ["tick", "The tick ran the jobs"],
  ["outbox", "The outbox's work was reconciled"],
] as const;
type Check = (typeof checks)[number][0];

// minutes says a number of minutes as people say it.
export function minutes(n: number): string {
  const unit = (k: number, one: string) => (k === 1 ? `1 ${one}` : `${k} ${one}s`);
  if (n < 60) return unit(n, "minute");
  if (n < 1440) return n % 60 === 0 ? unit(n / 60, "hour") : `${unit(Math.floor(n / 60), "hour")} ${unit(n % 60, "minute")}`;
  const days = Math.floor(n / 1440);
  const hours = Math.floor((n % 1440) / 60);
  return hours === 0 ? unit(days, "day") : `${unit(days, "day")} ${unit(hours, "hour")}`;
}

// RestoreDrillsTab is the restore drill register of Compliance › Controls (DR-06, CTL-RES-02 Q; p1-operations-dr design
// D2): each full drill run by the restore drill runbook, with what it restored, the data lost, the time taken and DR-06's
// checks; a drill counts as recovered only when every check passed. The DPO keeps it (Appendix D, Compliance Center: C/A).
export function RestoreDrillsTab() {
  const id = useId();
  const { session } = useSignedIn();
  const officer = (session.roles ?? []).includes("dpo");
  const list = useQuery({ queryKey: ["restore-drills"], queryFn: drills });
  const [recording, setRecording] = useState(false);
  if (list.isError) return <Refusal problem={problemOf(list.error)} title="The restore drills could not be shown" />;
  if (!list.data) return <p className="fy-small">Loading the restore drills…</p>;
  return (
    <>
      <section className="fy-card" aria-labelledby={`${id}-drills`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-drills`} title={helpText("compliance.restore-drills").tooltip}>
            Restore drills
          </h2>
          {officer && (
            <Button size="sm" onClick={() => setRecording(true)}>
              <Icon of={Plus} />
              Record a drill
            </Button>
          )}
        </div>
        <p className="fy-card__body fy-small fy-secondary">
          Each quarter the weekly export is restored into an isolated project, by the restore drill runbook. The platform also copies the database every four weeks and checks it by itself: that drill is the job restore.drill, in the
          controls.
        </p>
        {list.data.length === 0 ? (
          <p className="fy-card__body fy-small fy-secondary">No full drill recorded yet.</p>
        ) : (
          <ul className="fy-list" aria-labelledby={`${id}-drills`}>
            {list.data
              .slice()
              .reverse()
              .map((d) => {
                const failed = checks.filter(([k]) => !d[k]).map(([, words]) => words);
                return (
                  <li key={d.id} className="fy-list__item">
                    <span className="fy-list__main">
                      <span className="fy-list__title whitespace-normal">
                        {date(d.held_on)}: {scenarios[d.scenario]}
                      </span>
                      <span className="fy-list__meta whitespace-normal">
                        From {d.restored_from} · {minutes(d.data_lost_minutes)} of data lost · took {minutes(d.took_minutes)}
                      </span>
                      {failed.length > 0 && <span className="fy-list__meta whitespace-normal">Did not pass: {failed.join("; ")}</span>}
                      <span className="fy-list__meta whitespace-normal">{d.findings}</span>
                    </span>
                    {d.recovered ? (
                      <span className="fy-chip fy-chip--good">
                        <Icon of={CircleCheck} />
                        Recovered
                      </span>
                    ) : (
                      <span className="fy-chip fy-chip--critical">
                        <Icon of={CircleX} />
                        Not recovered
                      </span>
                    )}
                  </li>
                );
              })}
          </ul>
        )}
      </section>
      {recording && <DrillForm onClose={() => setRecording(false)} />}
    </>
  );
}

// DrillForm records a full restore drill (DR-06).
function DrillForm({ onClose }: { onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [form, setForm] = useState({ held_on: "", scenario: "project_loss" as Scenario, restored_from: "", data_lost: "", took: "", findings: "" });
  const [passed, setPassed] = useState<Record<Check, boolean>>({ ledger: false, audit_chain: false, documents: false, keys: false, sign_in: false, tick: false, outbox: false });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const save = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/restore-drills", {
          params: { header: { "Idempotency-Key": newKey() } },
          body: {
            held_on: form.held_on,
            scenario: form.scenario,
            restored_from: form.restored_from.trim(),
            data_lost_minutes: Number(form.data_lost),
            took_minutes: Number(form.took),
            findings: form.findings.trim(),
            ...passed,
          },
        }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await Promise.all(["restore-drills", "compliance-reviews"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  const whole = (v: string) => /^\d+$/.test(v);
  const ready = form.held_on !== "" && form.restored_from.trim() !== "" && whole(form.data_lost) && whole(form.took) && Number(form.took) > 0 && form.findings.trim() !== "";
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && !save.isPending && onClose()}
      dismissible={!save.isPending}
      eyebrow="Restore drills"
      title="Record a drill"
      description="Record what the restore drill runbook found. It counts as recovered only when every check passed. Never paste a secret, a token, a full NRIC or a bank number."
    >
      {problem && <Refusal problem={problem} title="The drill was not recorded" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-held`}>
            Held on
          </label>
          <input className="fy-input" type="date" id={`${id}-held`} value={form.held_on} onChange={(e) => setForm({ ...form, held_on: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-scenario`}>
            Scenario
          </label>
          <select className="fy-select" id={`${id}-scenario`} value={form.scenario} onChange={(e) => setForm({ ...form, scenario: e.target.value as Scenario })}>
            {(Object.keys(scenarios) as Scenario[]).map((s) => (
              <option key={s} value={s}>
                {scenarios[s]}
              </option>
            ))}
          </select>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-from`}>
            Restored from
          </label>
          <input className="fy-input" id={`${id}-from`} aria-describedby={`${id}-from-help`} value={form.restored_from} onChange={(e) => setForm({ ...form, restored_from: e.target.value })} />
          <span className="fy-help" id={`${id}-from-help`}>
            The weekly export, or the point in time the copy was made from.
          </span>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-lost`}>
            Data lost, in minutes
          </label>
          <input className="fy-input" type="number" min={0} id={`${id}-lost`} value={form.data_lost} onChange={(e) => setForm({ ...form, data_lost: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-took`}>
            Recovery took, in minutes
          </label>
          <input className="fy-input" type="number" min={1} id={`${id}-took`} value={form.took} onChange={(e) => setForm({ ...form, took: e.target.value })} />
        </div>
        <fieldset className="fy-field" title={helpText("restore-drills.checks").tooltip}>
          <legend className="fy-label">{helpText("restore-drills.checks").label}</legend>
          {checks.map(([k, words]) => (
            <label key={k} className="fy-check">
              <input type="checkbox" checked={passed[k]} onChange={(e) => setPassed({ ...passed, [k]: e.target.checked })} />
              {words}
            </label>
          ))}
        </fieldset>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-findings`}>
            What it found and what changed
          </label>
          <textarea className="fy-textarea" id={`${id}-findings`} value={form.findings} onChange={(e) => setForm({ ...form, findings: e.target.value })} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={save.isPending || !ready}>
            Record the drill
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
