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
import { minutes } from "./RestoreDrills";

type Drill = components["schemas"]["HeartbeatDrillView"];
type Environment = Drill["environment"];

async function drills() {
  const { data, error } = await api.GET("/v1/compliance/heartbeat-drills");
  if (error) throw error;
  return data.items;
}

// came says when an alert came after the pause, or that it never did.
function came(n: number | undefined): string {
  return n === undefined ? "never came" : `came after ${minutes(n)}`;
}

// HeartbeatDrillsTab is the heartbeat drill register of Compliance › Controls (NFR-OBS-02, CTL-RES-05 Q; p1-operations-dr
// design D6): each drill in which the tick was paused, with when the first alert and the escalation came; it passes when
// the platform owner was told within 30 minutes and the Owners within 60. The DPO keeps it (Compliance Center: C/A).
export function HeartbeatDrillsTab() {
  const id = useId();
  const { session } = useSignedIn();
  const officer = (session.roles ?? []).includes("dpo");
  const list = useQuery({ queryKey: ["heartbeat-drills"], queryFn: drills });
  const [recording, setRecording] = useState(false);
  if (list.isError) return <Refusal problem={problemOf(list.error)} title="The heartbeat drills could not be shown" />;
  if (!list.data) return <p className="fy-small">Loading the heartbeat drills…</p>;
  return (
    <>
      <section className="fy-card" aria-labelledby={`${id}-drills`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-drills`} title={helpText("compliance.heartbeat-drills").tooltip}>
            Heartbeat drills
          </h2>
          {officer && (
            <Button size="sm" onClick={() => setRecording(true)}>
              <Icon of={Plus} />
              Record a drill
            </Button>
          )}
        </div>
        <p className="fy-card__body fy-small fy-secondary">
          Each quarter the scheduled tick is paused in staging, by the heartbeat drill runbook, to prove that someone hears of it: the platform owner within 30 minutes, the Owners within an hour.
        </p>
        {list.data.length === 0 ? (
          <p className="fy-card__body fy-small fy-secondary">No heartbeat drill recorded yet.</p>
        ) : (
          <ul className="fy-list" aria-labelledby={`${id}-drills`}>
            {list.data
              .slice()
              .reverse()
              .map((d) => (
                <li key={d.id} className="fy-list__item">
                  <span className="fy-list__main">
                    <span className="fy-list__title whitespace-normal">
                      {date(d.held_on)}: the tick paused {minutes(d.paused_minutes)} in {d.environment}
                    </span>
                    <span className="fy-list__meta whitespace-normal">
                      The first alert {came(d.first_alert_minutes)} · the escalation {came(d.escalation_minutes)}
                    </span>
                    <span className="fy-list__meta whitespace-normal">{d.findings}</span>
                  </span>
                  {d.passed ? (
                    <span className="fy-chip fy-chip--good">
                      <Icon of={CircleCheck} />
                      Passed
                    </span>
                  ) : (
                    <span className="fy-chip fy-chip--critical">
                      <Icon of={CircleX} />
                      Not passed
                    </span>
                  )}
                </li>
              ))}
          </ul>
        )}
      </section>
      {recording && <DrillForm onClose={() => setRecording(false)} />}
    </>
  );
}

// DrillForm records a heartbeat drill (NFR-OBS-02).
function DrillForm({ onClose }: { onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [form, setForm] = useState({ held_on: "", environment: "staging" as Environment, paused: "", first: "", escalation: "", findings: "" });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const optional = (v: string) => (v.trim() === "" ? undefined : Number(v));
  const save = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/heartbeat-drills", {
          params: { header: { "Idempotency-Key": newKey() } },
          body: {
            held_on: form.held_on,
            environment: form.environment,
            paused_minutes: Number(form.paused),
            first_alert_minutes: optional(form.first),
            escalation_minutes: optional(form.escalation),
            findings: form.findings.trim(),
          },
        }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await Promise.all(["heartbeat-drills", "compliance-reviews"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  const whole = (v: string) => /^\d+$/.test(v);
  const blankOrWhole = (v: string) => v.trim() === "" || whole(v);
  const ready = form.held_on !== "" && whole(form.paused) && Number(form.paused) > 0 && blankOrWhole(form.first) && blankOrWhole(form.escalation) && form.findings.trim() !== "";
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && !save.isPending && onClose()}
      dismissible={!save.isPending}
      eyebrow="Heartbeat drills"
      title="Record a drill"
      description="Record what the heartbeat drill runbook found. It passes when the platform owner heard within 30 minutes and the Owners within 60."
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
          <label className="fy-label" htmlFor={`${id}-env`}>
            Where the tick was paused
          </label>
          <select className="fy-select" id={`${id}-env`} value={form.environment} onChange={(e) => setForm({ ...form, environment: e.target.value as Environment })}>
            <option value="staging">Staging</option>
            <option value="production">Production</option>
          </select>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-paused`}>
            Paused for, in minutes
          </label>
          <input className="fy-input" type="number" min={1} id={`${id}-paused`} value={form.paused} onChange={(e) => setForm({ ...form, paused: e.target.value })} />
        </div>
        <fieldset className="fy-field" title={helpText("heartbeat-drills.minutes").tooltip}>
          <legend className="fy-label">{helpText("heartbeat-drills.minutes").label}</legend>
          <label className="fy-label" htmlFor={`${id}-first`}>
            The first alert, minutes after the pause
          </label>
          <input className="fy-input" type="number" min={0} id={`${id}-first`} value={form.first} onChange={(e) => setForm({ ...form, first: e.target.value })} />
          <label className="fy-label" htmlFor={`${id}-escalation`}>
            The escalation, minutes after the pause
          </label>
          <input className="fy-input" type="number" min={0} id={`${id}-escalation`} value={form.escalation} onChange={(e) => setForm({ ...form, escalation: e.target.value })} />
          <span className="fy-help">Leave one empty when that alert never came.</span>
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
