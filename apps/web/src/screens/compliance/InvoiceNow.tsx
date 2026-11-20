import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, CircleDashed, ClockAlert, Download, Send, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Gate = components["schemas"]["InvoiceNowGate"];
type Submission = components["schemas"]["InvoiceNowSubmission"];
type Inbound = components["schemas"]["InvoiceNowInbound"];

// Who sends, excludes and asks for a contingency: C on GST returns (the Accountant and the Tax agent); who approves one: A (the Owner and the Finance controller); who
// receives invoices and makes draft bills: C on Bills (Appendix D).
const preparers = ["accountant", "tax_agent"];
const approvers = ["owner", "controller"];
const billers = ["accountant", "bookkeeper", "ap_clerk"];

const quarterStart = (d: Date) => new Date(Date.UTC(d.getUTCFullYear(), Math.floor(d.getUTCMonth() / 3) * 3, 1));
const iso = (d: Date) => d.toISOString().slice(0, 10);
const defaultPeriod = () => {
  const s = quarterStart(new Date());
  return { from: iso(s), to: iso(new Date(Date.UTC(s.getUTCFullYear(), s.getUTCMonth() + 3, 0))) };
};

// The gate's states, in plain words, as a chip.
const gateWords: Record<Gate["state"], [string, string, typeof CircleCheck]> = {
  ready: ["Ready to file", "fy-chip--good", CircleCheck],
  blocked: ["Waiting for submissions", "fy-chip--serious", ClockAlert],
  not_assessed: ["Not assessed", "fy-chip--neutral", CircleDashed],
};

const stateWords: Record<string, string> = { none: "Not sent", queued: "Queued", sent: "Sent", failed: "Failed", acknowledged: "Acknowledged" };

async function gate(from: string, to: string): Promise<Gate> {
  const { data, error } = await api.GET("/v1/compliance/invoicenow-gate", { params: { query: { from, to } } });
  if (error) throw error;
  return data;
}

async function submissions(): Promise<Submission[]> {
  const { data, error } = await api.GET("/v1/invoicenow/submissions", { params: { query: { page_size: 100 } } });
  if (error) throw error;
  return data.items ?? [];
}

async function received(): Promise<Inbound[]> {
  const { data, error } = await api.GET("/v1/invoicenow/inbound", { params: { query: { page_size: 100 } } });
  if (error) throw error;
  return data.items ?? [];
}

// InvoiceNow is the submissions of the documents behind the GST return (FR-AR-12, FR-AP-05, FR-TAX-10, FR-TAX-11): the filing gate for a period, each submission up
// to IRAS's acknowledgement ID, and the invoices suppliers sent. It shows what the server answers and never works anything out itself (FR-UX-22).
export function InvoiceNow() {
  const { session } = useSignedIn();
  const roles = session.roles ?? [];
  const mayPrepare = roles.some((r) => preparers.includes(r));
  const mayApprove = roles.some((r) => approvers.includes(r));
  const mayReceive = roles.some((r) => billers.includes(r));
  const confirm = useConfirm();
  const queries = useQueryClient();
  const [period, setPeriod] = useState(defaultPeriod);
  const [busy, setBusy] = useState("");
  const [status, setStatus] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [contingency, setContingency] = useState({ reason: "", evidence: "" });
  const g = useQuery({ queryKey: ["invoicenow-gate", period.from, period.to], queryFn: () => gate(period.from, period.to) });
  const s = useQuery({ queryKey: ["invoicenow-submissions"], queryFn: submissions });
  const r = useQuery({ queryKey: ["invoicenow-inbound"], queryFn: received });
  const refresh = async () => {
    await queries.invalidateQueries({ queryKey: ["invoicenow-gate"] });
    await queries.invalidateQueries({ queryKey: ["invoicenow-submissions"] });
    await queries.invalidateQueries({ queryKey: ["invoicenow-inbound"] });
  };
  const run = async (what: string, call: () => Promise<{ data?: unknown; error?: unknown }>, done: string) => {
    setBusy(what);
    setProblem(null);
    setStatus("");
    const out = await guarded(confirm, call);
    setBusy("");
    if (!out.data) return setProblem(problemOf(out.error));
    setStatus(done);
    await refresh();
  };
  const queue = (document_id: string) =>
    run(`queue-${document_id}`, () => api.POST("/v1/invoicenow/submissions", { params: { header: { "Idempotency-Key": newKey() } }, body: { document_id } }), "Queued: send it now, or the next run sends it.");
  const send = (id: string) =>
    run(`send-${id}`, () => api.POST("/v1/invoicenow/submissions/{submission_id}/send", { params: { path: { submission_id: id }, header: { "Idempotency-Key": newKey() } } }), "Asked the access point.");
  const retry = (id: string) =>
    run(`retry-${id}`, () => api.POST("/v1/invoicenow/submissions/{submission_id}/retry", { params: { path: { submission_id: id }, header: { "Idempotency-Key": newKey() } } }), "Queued again.");
  const poll = () => run("poll", () => api.POST("/v1/invoicenow/inbound/poll", { params: { header: { "Idempotency-Key": newKey() } } }), "Received what the access point holds.");
  const draft = (id: string) =>
    run(`draft-${id}`, () => api.POST("/v1/invoicenow/inbound/{inbound_id}/draft", { params: { path: { inbound_id: id }, header: { "Idempotency-Key": newKey() } } }), "Tried to make the draft bill.");
  const ask = () =>
    run("contingency", () => api.POST("/v1/invoicenow/contingencies", { params: { header: { "Idempotency-Key": newKey() } }, body: { period_from: period.from, period_to: period.to, ...contingency } }),
      "Asked: someone else approves it before the return may be filed with the gap.");
  const x = g.data;
  const missing = x?.missing ?? [];
  const chip = x ? gateWords[x.state] : undefined;
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Compliance</span>
          <h1 className="fy-h1" title={helpText("compliance.invoicenow").tooltip}>
            InvoiceNow
          </h1>
          <p className="fy-secondary">The invoices behind the GST return, sent to IRAS through InvoiceNow, and the invoices suppliers sent to you.</p>
        </div>
      </div>
      <p className="fy-small" role="status">
        {status}
      </p>
      {problem && <Refusal problem={problem} title="That was not done" />}
      <section className="fy-card" aria-labelledby="gate-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="gate-title" title={helpText("invoicenow.gate").tooltip}>
            Filing gate
          </h2>
          {chip && (
            <span className={`fy-chip ${chip[1]}`}>
              <Icon of={chip[2]} />
              {chip[0]}
            </span>
          )}
        </div>
        <div className="fy-card__body fy-stack fy-stack--4">
          <div className="fy-row fy-row--gap">
            <label className="fy-field">
              <span className="fy-label">From</span>
              <input className="fy-input" type="date" value={period.from} onChange={(e) => setPeriod({ ...period, from: e.target.value })} />
            </label>
            <label className="fy-field">
              <span className="fy-label">To</span>
              <input className="fy-input" type="date" value={period.to} onChange={(e) => setPeriod({ ...period, to: e.target.value })} />
            </label>
          </div>
          {g.isError && <Refusal problem={problemOf(g.error)} title="The gate could not be shown" />}
          {g.isPending && <p className="fy-secondary">Loading the gate…</p>}
          {x?.reason && <p className="fy-small">{x.reason}</p>}
          {x && x.state !== "not_assessed" && (
            <p className="fy-small fy-secondary">
              {x.documents} document{x.documents === 1 ? "" : "s"} of the period: {x.acknowledged} acknowledged by IRAS, {x.excluded} left out, {missing.length} still to do.
            </p>
          )}
          {x?.contingency && (
            <div className="fy-banner fy-banner--warning" role="status">
              <Icon of={TriangleAlert} size={20} />
              <div className="fy-banner__text">
                <strong>The return may be filed with this gap</strong>
                <span className="fy-small">
                  {x.contingency.reason} Asked by {x.contingency.requested_by}, approved by {x.contingency.approved_by}.
                </span>
              </div>
            </div>
          )}
          {missing.length > 0 && (
            <ul className="fy-list" aria-label="Documents the gate waits for">
              {missing.map((m) => (
                <li key={m.document_id} className="fy-list__item fy-row fy-row--between">
                  <span>
                    {m.number || "(no number)"} <span className="fy-small fy-muted">{date(m.date)}, {stateWords[m.state] ?? m.state}</span>
                    {m.needs_choice && <span className="fy-small"> The tax agent chooses ES33 or ESN33 first (Settings › Organisation).</span>}
                  </span>
                  {mayPrepare && m.state === "none" && m.kind !== "purchase" && (
                    <Button size="sm" onClick={() => void queue(m.document_id)} disabled={busy !== ""}>
                      <Icon of={Send} />
                      Queue for InvoiceNow
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          )}
          {x?.state === "blocked" && !x.contingency && mayPrepare && (
            <details className="fy-stack fy-stack--2">
              <summary title={helpText("invoicenow.contingency").tooltip}>The access point or IRAS was down: ask to file with the gap</summary>
              <label className="fy-field">
                <span className="fy-label">Why the return cannot wait</span>
                <input className="fy-input" value={contingency.reason} onChange={(e) => setContingency({ ...contingency, reason: e.target.value })} />
              </label>
              <label className="fy-field">
                <span className="fy-label">The outage and any IRAS correspondence</span>
                <textarea className="fy-input" value={contingency.evidence} onChange={(e) => setContingency({ ...contingency, evidence: e.target.value })} />
              </label>
              <Button size="sm" onClick={() => void ask()} disabled={busy !== "" || contingency.reason.length < 10 || contingency.evidence.length < 10}>
                Ask someone else to approve
              </Button>
            </details>
          )}
          {x?.state === "blocked" && mayApprove && !mayPrepare && (
            <p className="fy-small fy-muted">A contingency someone asked for is approved with the API (approveInvoiceNowContingency) until its button follows.</p>
          )}
        </div>
      </section>
      <section className="fy-card" aria-labelledby="sub-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="sub-title">
            Submissions
          </h2>
        </div>
        <div className="fy-card__body">
          {s.isError && <Refusal problem={problemOf(s.error)} title="The submissions could not be shown" />}
          {s.data && s.data.length === 0 && <p className="fy-secondary">Nothing has been queued yet.</p>}
          {s.data && s.data.length > 0 && (
            <table className="fy-table">
              <caption className="fy-sr-only">InvoiceNow submissions</caption>
              <thead>
                <tr>
                  <th scope="col">Document</th>
                  <th scope="col">Where</th>
                  <th scope="col">Status</th>
                  <th scope="col">IRAS acknowledgement</th>
                  <th scope="col">
                    <span className="fy-sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {s.data.map((x) => (
                  <tr key={x.id}>
                    <td>
                      {x.document_uuid.slice(0, 8)} v{x.version}
                    </td>
                    <td>{x.kind === "peppol" ? x.recipient : "To IRAS alone"}</td>
                    <td>
                      {stateWords[x.status] ?? x.status}
                      {x.last_error && <span className="fy-small fy-muted"> {x.last_error}</span>}
                    </td>
                    <td className="fy-num">{x.iras_ack ?? ""}</td>
                    <td>
                      {mayPrepare && (x.status === "queued" || x.status === "sent") && (
                        <Button size="sm" variant="ghost" onClick={() => void send(x.id)} disabled={busy !== ""}>
                          {x.status === "queued" ? "Send now" : "Check status"}
                        </Button>
                      )}
                      {mayPrepare && x.status === "failed" && (
                        <Button size="sm" variant="ghost" onClick={() => void retry(x.id)} disabled={busy !== ""}>
                          Queue again
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </section>
      <section className="fy-card" aria-labelledby="rx-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="rx-title" title={helpText("invoicenow.receive").tooltip}>
            Received from suppliers
          </h2>
          {mayReceive && (
            <Button size="sm" onClick={() => void poll()} disabled={busy !== ""} title={helpText("invoicenow.receive").tooltip}>
              <Icon of={Download} />
              Receive now
            </Button>
          )}
        </div>
        <div className="fy-card__body">
          {r.isError && <Refusal problem={problemOf(r.error)} title="The received invoices could not be shown" />}
          {r.data && r.data.length === 0 && <p className="fy-secondary">Nothing has been received.</p>}
          {r.data && r.data.length > 0 && (
            <ul className="fy-list" aria-label="Received invoices">
              {r.data.map((x) => (
                <li key={x.id} className="fy-list__item fy-row fy-row--between">
                  <span>
                    {x.number || "(no number)"} <span className="fy-small fy-muted">from {x.sender}</span>
                    <br />
                    <span className="fy-small">
                      {x.status === "drafted" ? "A draft bill was made." : x.status === "refused" ? "Refused: " + (x.note ?? "") : "Waiting: " + (x.note ?? "")}
                    </span>
                  </span>
                  {mayReceive && x.status === "received" && (
                    <Button size="sm" variant="ghost" onClick={() => void draft(x.id)} disabled={busy !== ""}>
                      Make the draft bill
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      </section>
    </div>
  );
}
