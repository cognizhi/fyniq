import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { problemOf, type ApiProblem } from "../../ui";

type Feature = "records" | "payroll" | "leave" | "letters";
const features: Record<Feature, string> = { records: "People records", payroll: "Payroll", leave: "Leave", letters: "Letters and self-service" };

// LinkRequest asks to link Nemura (FR-PPL-01): what Nemura's link page shows once it has proposed the link. The Owner and a second approver approve it; nothing is linked by asking.
export function LinkRequest({ onDone }: { onDone: (what: string) => void }) {
  const id = useId();
  const h = helpText("people.link");
  const confirm = useConfirm();
  const [text, setText] = useState({ nemura_organisation_id: "", nemura_origin: "", nemura_client_id: "", nemura_ask_client_id: "", link_key: "", client_name: "Nemura link", client_public_key: "", expires_at: "" });
  const [on, setOn] = useState<Feature[]>(["records", "payroll"]);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const set = (k: keyof typeof text) => (e: { target: { value: string } }) => setText({ ...text, [k]: e.target.value });
  const run = async (e: FormEvent) => {
    e.preventDefault();
    setProblem(null);
    let key: components["schemas"]["PeopleLinkRequest"]["client_public_key"];
    try {
      key = JSON.parse(text.client_public_key);
    } catch {
      return setProblem({ status: 422, code: "API-INPUT-02", detail: "The client's public key is not JSON: paste the JWK Nemura's link page shows.", fix: "Paste the key as shown." } as ApiProblem);
    }
    setBusy(true);
    const out = await guarded(confirm, () =>
      api.POST("/v1/people-link", {
        params: { header: { "Idempotency-Key": newKey() } },
        body: { ...text, client_public_key: key, expires_at: new Date(text.expires_at).toISOString(), features: on },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone("Asked to link Nemura. The Owner and a second approver approve it, and then Nemura approves its own half.");
  };
  const field = (k: keyof typeof text, label: string, hint?: string, multiline = false) => (
    <div className="fy-field">
      <label className="fy-label" htmlFor={`${id}-${k}`}>
        {label}
      </label>
      {multiline ? <textarea className="fy-input" id={`${id}-${k}`} rows={4} value={text[k]} onChange={set(k)} /> : <input className="fy-input" id={`${id}-${k}`} value={text[k]} onChange={set(k)} />}
      {hint && <span className="fy-hint">{hint}</span>}
    </div>
  );
  return (
    <form className="fy-stack fy-stack--3" onSubmit={(e) => void run(e)} noValidate aria-label="Link Nemura">
      {problem && <Refusal problem={problem} title="The link was not asked for" />}
      {field("nemura_organisation_id", "Nemura organisation ID", "Shown on Nemura's link page once it has proposed the link.")}
      {field("nemura_origin", "Nemura's web address", "For example https://people.example.com: where its people sign passkey approvals.")}
      {field("nemura_client_id", "The ID Fyniq's client will have at Nemura")}
      {field("nemura_ask_client_id", "The ID Fyniq's read-only client will have at Nemura")}
      {field("link_key", "Nemura's link key", "Its Ed25519 public key, base64url.")}
      {field("client_name", "Client name")}
      {field("client_public_key", "Nemura's client key (JWK)", "The public key Nemura's client signs its token request with.", true)}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-expires_at`}>
          The client expires on
        </label>
        <input className="fy-input" id={`${id}-expires_at`} type="date" value={text.expires_at} onChange={set("expires_at")} />
        <span className="fy-hint">Within a year; link again before then.</span>
      </div>
      <fieldset className="fy-field">
        <legend className="fy-label">What lives in Nemura from now on</legend>
        {(Object.keys(features) as Feature[]).map((f) => (
          <label key={f} className="fy-row gap-2">
            <input type="checkbox" checked={on.includes(f)} onChange={(e) => setOn(e.target.checked ? [...on, f] : on.filter((x) => x !== f))} /> {features[f]}
          </label>
        ))}
      </fieldset>
      <div className="fy-row justify-end gap-2">
        <Button variant="primary" type="submit" disabled={busy || on.length === 0 || !text.expires_at} title={h.tooltip}>
          {h.label}
        </Button>
      </div>
    </form>
  );
}

// Unlink asks to end the link (FR-PPL-16), and, once it is unlinking, shows the work in flight and finishes it when nothing is left.
export function Unlink({ state, inFlight, onDone }: { state: string; inFlight?: components["schemas"]["PeopleLinkInFlight"]; onDone: (what: string) => void }) {
  const id = useId();
  const h = helpText("people.unlink");
  const confirm = useConfirm();
  const [reason, setReason] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const ask = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => api.POST("/v1/people-link/unlink", { params: { header: { "Idempotency-Key": newKey() } }, body: { reason: reason.trim() } }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone("Asked to unlink Nemura. The Owner and a second approver approve it; work in flight settles first.");
  };
  const finish = async () => {
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/people-link/unlink/finish", { params: { header: { "Idempotency-Key": newKey() } } });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone("The link has ended. Both sides keep their data.");
  };
  if (state === "unlinking") {
    return (
      <div className="fy-stack fy-stack--2">
        {problem && <Refusal problem={problem} title="The link was not ended" />}
        <p>
          Unlinking waits for work in flight: {inFlight?.salary_batches ?? 0} salary batch(es) not settled, {inFlight?.unknown_outcomes ?? 0} payment(s) with an unknown outcome, {inFlight?.events_waiting ? "events waiting to go to Nemura" : "no events waiting"}.
        </p>
        <div className="fy-row gap-2">
          <Button variant="primary" type="button" onClick={() => void finish()} disabled={busy}>
            Finish unlinking
          </Button>
        </div>
      </div>
    );
  }
  if (state !== "active" && state !== "suspended") return null;
  return (
    <form className="fy-stack fy-stack--2" onSubmit={(e) => void ask(e)} noValidate aria-label="Unlink Nemura">
      {problem && <Refusal problem={problem} title="Unlinking was not asked for" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-reason`}>
          Why
        </label>
        <input className="fy-input" id={`${id}-reason`} value={reason} onChange={(e) => setReason(e.target.value)} />
      </div>
      <div className="fy-row justify-end gap-2">
        <Button variant="secondary" type="submit" disabled={busy || !reason.trim()} title={h.tooltip}>
          {h.label}
        </Button>
      </div>
    </form>
  );
}

const sourceWords: Record<string, string> = { nemura: "Nemura", external: "An external system (a payroll provider's file)", none: "None: posted by manual journal" };

// SourceChange asks to move where one feature lives (FR-PPL-16): the Owner and a second approver approve it, and nothing moves by asking.
export function SourceChange({ linkState, onDone }: { linkState: string; onDone: (what: string) => void }) {
  const id = useId();
  const h = helpText("people.source");
  const confirm = useConfirm();
  const [feature, setFeature] = useState<Feature>("payroll");
  const [source, setSource] = useState<"nemura" | "external" | "none">("external");
  const [from, setFrom] = useState("");
  const [reason, setReason] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const run = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/people-link/sources", { params: { header: { "Idempotency-Key": newKey() } }, body: { feature, source, effective_from: from, reason: reason.trim() } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(`Asked to move ${features[feature].toLowerCase()} to ${(sourceWords[source] ?? source).toLowerCase()} from ${from}. The Owner and a second approver approve it.`);
  };
  return (
    <form className="fy-stack fy-stack--2" onSubmit={(e) => void run(e)} noValidate aria-label="Move a feature">
      {problem && <Refusal problem={problem} title="The move was not asked for" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-feature`}>
          Feature
        </label>
        <select className="fy-select" id={`${id}-feature`} value={feature} onChange={(e) => setFeature(e.target.value as Feature)}>
          {(Object.keys(features) as Feature[]).map((f) => (
            <option key={f} value={f}>
              {features[f]}
            </option>
          ))}
        </select>
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-source`}>
          Lives in
        </label>
        <select className="fy-select" id={`${id}-source`} value={source} onChange={(e) => setSource(e.target.value as typeof source)}>
          {Object.entries(sourceWords).map(([k, v]) => (
            <option key={k} value={k} disabled={k === "nemura" && linkState !== "active"}>
              {v}
            </option>
          ))}
        </select>
        {linkState !== "active" && <span className="fy-hint">Nemura needs an active link.</span>}
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-from`}>
          From
        </label>
        <input className="fy-input" id={`${id}-from`} type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-reason`}>
          Why
        </label>
        <input className="fy-input" id={`${id}-reason`} value={reason} onChange={(e) => setReason(e.target.value)} />
      </div>
      <div className="fy-row justify-end gap-2">
        <Button variant="secondary" type="submit" disabled={busy || !from || !reason.trim()} title={h.tooltip}>
          {h.label}
        </Button>
      </div>
    </form>
  );
}
