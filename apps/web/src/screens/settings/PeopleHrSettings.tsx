import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, CircleX, Hourglass, Link2Off, Users } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { LinkRequest, Unlink } from "./LinkForms";

type Item = components["schemas"]["PayrollArrangement"]["item"];
type Method = components["schemas"]["PayrollArrangementChange"]["method"];

const features: Record<string, string> = { records: "People records", payroll: "Payroll", leave: "Leave", letters: "Letters and self-service" };
const sources: Record<string, string> = { nemura: "Nemura", external: "An external system", none: "None: posted by manual journal" };
const items: Record<Item, string> = { payslips: "Itemised payslips", kets: "Key employment terms", cpf_sdl_shg: "CPF, SDL and SHG submission and payment", ais: "Auto-Inclusion Scheme", ir21: "IR21 clearance" };
const methods: Record<Method, string> = { nemura: "Nemura issues it", provider: "A payroll provider", manual: "A documented manual process", not_applicable: "Does not apply" };

function LinkState({ state }: { state: string }) {
  const tone = state === "active" ? "good" : state === "none" || state === "unlinked" ? "neutral" : "warning";
  return (
    <span className={`fy-chip fy-chip--${tone}`}>
      <Icon of={state === "active" ? CircleCheck : state === "none" || state === "unlinked" ? Link2Off : Hourglass} />
      {state === "none" ? "Not linked" : state.charAt(0).toUpperCase() + state.slice(1)}
    </span>
  );
}

// Arrange records how one item of the payroll go-live gate is met, as evidence (FR-PAY-15).
function Arrange({ month, onDone }: { month: string; onDone: (what: string) => void }) {
  const id = useId();
  const h = helpText("people.arrange");
  const confirm = useConfirm();
  const [item, setItem] = useState<Item>("payslips");
  const [method, setMethod] = useState<Method>("provider");
  const [provider, setProvider] = useState("");
  const [note, setNote] = useState("");
  const [evidence, setEvidence] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const run = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/payroll-arrangements", {
        params: { header: { "Idempotency-Key": newKey() } },
        body: { item, method, provider: provider.trim() || undefined, note: note.trim() || undefined, evidence_id: method === "nemura" ? undefined : evidence.trim(), effective_from: month },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(`Recorded how ${items[item].toLowerCase()} is met from ${month}.`);
  };
  return (
    <form className="fy-stack fy-stack--3" onSubmit={(e) => void run(e)} noValidate aria-label="Record an arrangement">
      {problem && <Refusal problem={problem} title="The arrangement was not recorded" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-item`}>
          What it covers
        </label>
        <select className="fy-select" id={`${id}-item`} value={item} onChange={(e) => setItem(e.target.value as Item)}>
          {Object.entries(items).map(([k, v]) => (
            <option key={k} value={k}>
              {v}
            </option>
          ))}
        </select>
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-method`}>
          How it is met
        </label>
        <select className="fy-select" id={`${id}-method`} value={method} onChange={(e) => setMethod(e.target.value as Method)}>
          {Object.entries(methods).map(([k, v]) => (
            <option key={k} value={k}>
              {v}
            </option>
          ))}
        </select>
      </div>
      {method === "provider" && (
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-provider`}>
            The provider
          </label>
          <input className="fy-input" id={`${id}-provider`} value={provider} onChange={(e) => setProvider(e.target.value)} />
        </div>
      )}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-note`}>
          {method === "not_applicable" ? "Why it does not apply" : "Note"}
        </label>
        <input className="fy-input" id={`${id}-note`} value={note} maxLength={500} onChange={(e) => setNote(e.target.value)} />
      </div>
      {method !== "nemura" && (
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-evidence`}>
            Evidence file ID
          </label>
          <input className="fy-input" id={`${id}-evidence`} value={evidence} onChange={(e) => setEvidence(e.target.value)} />
          <span className="fy-hint">The file that shows it: a contract with the provider, the documented process, a bank advice.</span>
        </div>
      )}
      <div className="fy-row justify-end gap-2">
        <Button variant="primary" type="submit" disabled={busy || (method !== "nemura" && !evidence.trim())} title={h.tooltip}>
          {h.label}
        </Button>
      </div>
    </form>
  );
}

// LinkHealth shows how the link stands, by identity (FR-PPL-08, FR-PPL-15; NM-INT-04): what Nemura says it queued against what Fyniq received, the runs of sequence numbers Nemura delivered and Fyniq never
// received, and one action that asks Nemura to send them again. It holds no payload, name or figure.
function LinkHealth({ onDone }: { onDone: (what: string) => void }) {
  const queries = useQueryClient();
  const h = helpText("people.health");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const health = useQuery({
    queryKey: ["people-link-health"],
    queryFn: async () => {
      const r = await api.GET("/v1/people-link/health", {});
      if (r.error) throw r.error;
      return r.data;
    },
  });
  if (health.isError) return <Refusal problem={problemOf(health.error)} title="The link's health could not be read" />;
  const d = health.data;
  if (!d) return <p className="fy-secondary">Checking the link…</p>;
  const repair = async () => {
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/people-link/repair", { params: { header: { "Idempotency-Key": newKey() } } });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(out.data.requested.length === 0 ? "Nothing was missing." : `Asked Nemura to send ${out.data.requested.length === 1 ? "one run" : `${out.data.requested.length} runs`} of events again.`);
    void queries.invalidateQueries({ queryKey: ["people-link-health"] });
  };
  return (
    <section aria-label="Link health" className="fy-stack fy-stack--2">
      <h2 className="fy-h2" title={h.tooltip}>
        {h.label}
      </h2>
      {problem && <Refusal problem={problem} title="Nemura could not be asked" />}
      {!d.reachable ? (
        <div className="fy-banner fy-banner--warning" role="note">
          <Icon of={Hourglass} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">Nemura's log could not be read.</span>
            <span className="fy-small">{d.note}</span>
          </div>
        </div>
      ) : (
        <>
          <ul className="fy-stack fy-stack--1">
            <li>
              Nemura has queued {d.nemura_highest_sequence} events for Fyniq; Fyniq's inbox holds up to {d.received_highest_sequence}.
            </li>
            <li>
              {d.waiting} waiting to be delivered, {d.failing} failing, {d.refused} refused.
            </li>
          </ul>
          {d.requested.length > 0 && (
            <p className="fy-secondary">
              <Icon of={Hourglass} /> Asked Nemura to send these again in the last day: {d.requested.map((m) => (m.from === m.to ? `${m.from}` : `${m.from} to ${m.to}`)).join(", ")}. Nemura sends the current state at new numbers, so these numbers do not arrive; they show as missing again after a day.
            </p>
          )}
          {d.missing.length === 0 ? (
            d.requested.length === 0 && (
              <p className="fy-secondary">
                <Icon of={CircleCheck} /> Nothing is missing: every event Nemura says it delivered reached Fyniq.
              </p>
            )
          ) : (
            <div className="fy-banner fy-banner--warning" role="note">
              <Icon of={CircleX} size={20} />
              <div className="fy-banner__text">
                <span className="fy-strong">
                  Nemura says it delivered events that Fyniq never received: {d.missing.map((m) => (m.from === m.to ? `${m.from}` : `${m.from} to ${m.to}`)).join(", ")}.
                </span>
                <span className="fy-small">Ask Nemura to send them again: it sends the current state of what they were about, never an old payload.</span>
              </div>
              <div className="fy-banner__action">
                <Button variant="secondary" size="sm" type="button" onClick={() => void repair()} disabled={busy}>
                  Ask Nemura to send them again
                </Button>
              </div>
            </div>
          )}
          {d.truncated && <p className="fy-secondary">Nemura's log is longer than one look reads: the oldest part was left out.</p>}
        </>
      )}
      <div className="fy-row gap-2">
        <Button variant="ghost" size="sm" type="button" onClick={() => void health.refetch()} disabled={health.isFetching}>
          Check again
        </Button>
      </div>
    </section>
  );
}

// PeopleHrSettings is Settings › People and HR (docs/design/screens PeopleHrSettings; FR-PPL-01, FR-PPL-08, FR-PAY-15): where each feature lives, the link's state and what it received, and how the
// month's statutory payroll outputs are met. Linking, changing a source and unlinking need the Owner and a second approver and are asked for through the API (Left: their forms).
export function PeopleHrSettings() {
  useSignedIn();
  const queries = useQueryClient();
  const [status, setStatus] = useState("");
  const month = new Date().toISOString().slice(0, 7);
  const link = useQuery({
    queryKey: ["people-link"],
    queryFn: async () => {
      const r = await api.GET("/v1/people-link", {});
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const gate = useQuery({
    queryKey: ["payroll-arrangements", month],
    queryFn: async () => {
      const r = await api.GET("/v1/payroll-arrangements", { params: { query: { month } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const imports = useQuery({
    queryKey: ["payroll-imports"],
    queryFn: async () => {
      const r = await api.GET("/v1/payroll-imports", {});
      if (r.error) throw r.error;
      return r.data.items;
    },
  });
  if (link.isError) return <Refusal problem={problemOf(link.error)} title="People and HR could not be shown" />;
  const l = link.data;
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Settings</span>
          </nav>
          <h1 className="fy-h1">People and HR</h1>
          <p className="fy-secondary">Where people records, payroll, leave and letters live, and how the month's statutory payroll outputs are met.</p>
        </div>
        <div className="fy-pagehead__actions">
          <Icon of={Users} />
          {l && <LinkState state={l.state} />}
        </div>
      </div>
      <ReviewBanner screen="people-hr-settings" />
      {status && (
        <div className="fy-banner fy-banner--good" role="status">
          <Icon of={CircleCheck} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">{status}</span>
          </div>
        </div>
      )}
      {!l && <p className="fy-secondary">Loading People and HR…</p>}
      {l && (
        <>
          <section aria-label="Where each feature lives" className="fy-stack fy-stack--2">
            <h2 className="fy-h2">Where each feature lives</h2>
            <table className="fy-table" aria-label="Sources">
              <thead>
                <tr>
                  <th scope="col">Feature</th>
                  <th scope="col">Lives in</th>
                  <th scope="col">From</th>
                </tr>
              </thead>
              <tbody>
                {Object.keys(features).map((f) => {
                  const s = l.sources.find((x) => x.feature === f);
                  return (
                    <tr key={f}>
                      <th scope="row">{features[f]}</th>
                      <td>{sources[s?.source ?? "none"]}</td>
                      <td>{s ? s.effective_from : "Not set"}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            {l.state !== "none" && (
              <p className="fy-secondary">
                Linked since {l.linked_at ? dateTime(l.linked_at) : "an earlier date"}, epoch {l.epoch}. Linking, changing where a feature lives and unlinking need the Owner and a second approver.
              </p>
            )}
          </section>
          {l.state === "active" && <LinkHealth onDone={setStatus} />}
          <section aria-label="Link or unlink" className="fy-stack fy-stack--2">
            <h2 className="fy-h2">{l.state === "none" || l.state === "unlinked" ? "Link Nemura" : "Unlink Nemura"}</h2>
            {l.state === "none" || l.state === "unlinked" ? (
              <LinkRequest onDone={(w) => { setStatus(w); void queries.invalidateQueries({ queryKey: ["people-link"] }); }} />
            ) : (
              <Unlink state={l.state} inFlight={l.in_flight ?? undefined} onDone={(w) => { setStatus(w); void queries.invalidateQueries({ queryKey: ["people-link"] }); }} />
            )}
          </section>
          {l.inbox.length > 0 && (
            <section aria-label="What Fyniq received" className="fy-stack fy-stack--2">
              <h2 className="fy-h2">What Fyniq received from Nemura</h2>
              <ul className="fy-stack fy-stack--1">
                {l.inbox.map((i) => (
                  <li key={i.outcome} className="fy-row gap-2">
                    <Icon of={i.outcome === "refused" ? CircleX : CircleCheck} />
                    <span>
                      {i.count} {i.outcome === "applied" ? "applied" : i.outcome === "ignored_older" ? "ignored as older" : "refused"}
                      {i.last_at ? `, the last ${dateTime(i.last_at)}` : ""}
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          )}
        </>
      )}
      <section aria-label="Payroll arrangements" className="fy-stack fy-stack--2">
        <h2 className="fy-h2">How {month}'s statutory payroll outputs are met</h2>
        {gate.data && (
          <>
            {gate.data.met ? (
              <p className="fy-secondary">Every item has an arrangement in force: a pay run can be approved for payment.</p>
            ) : (
              <div className="fy-banner fy-banner--critical" role="note">
                <Icon of={CircleX} size={20} />
                <div className="fy-banner__text">
                  <span className="fy-strong">A pay run cannot be approved for payment: {gate.data.missing.map((m) => items[m as Item]?.toLowerCase() ?? m).join(", ")} not recorded.</span>
                </div>
              </div>
            )}
            <ul className="fy-stack fy-stack--1">
              {gate.data.in_force.map((a) => (
                <li key={a.id}>
                  <span className="fy-strong">{items[a.item]}</span>: {methods[a.method]}
                  {a.provider ? `, ${a.provider}` : ""}, from {a.effective_from}
                </li>
              ))}
            </ul>
          </>
        )}
        <Arrange
          month={month}
          onDone={(what) => {
            setStatus(what);
            void queries.invalidateQueries({ queryKey: ["payroll-arrangements"] });
          }}
        />
      </section>
      {imports.data && imports.data.length > 0 && (
        <section aria-label="Payroll imports" className="fy-stack fy-stack--2">
          <h2 className="fy-h2">Provider payroll journals</h2>
          <ul className="fy-stack fy-stack--1">
            {imports.data.map((i) => (
              <li key={i.id}>
                {i.source_system} {i.period}, version {i.version}: a draft journal of {i.lines} lines, approved under Journals
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
