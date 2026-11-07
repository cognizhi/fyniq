import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, CircleDashed, ClockAlert, FileSignature, Info, Stamp, TriangleAlert } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { EvidencePanel } from "../accounting/JournalEvidence";

type Monitor = components["schemas"]["GSTMonitor"];
type YearTest = components["schemas"]["GSTYearTest"];
type Forecast = components["schemas"]["GSTForecastView"];
type ForecastDraft = components["schemas"]["GSTForecastDraft"];

// Who prepares a forecast and records it: C on GST returns, the Accountant and the Tax agent (Appendix D).
const preparers = ["accountant", "tax_agent"];

async function monitor(): Promise<Monitor> {
  const { data, error } = await api.GET("/v1/compliance/gst-monitor", {});
  if (error) throw error;
  return data;
}

async function forecasts(): Promise<Forecast[]> {
  const { data, error } = await api.GET("/v1/gst-forecasts", { params: { query: { page_size: 50 } } });
  if (error) throw error;
  return data.items ?? [];
}

// The bar's width in eighths, as Home's set-up progress draws it.
const eighths = ["w-0", "w-1/8", "w-1/4", "w-3/8", "w-1/2", "w-5/8", "w-3/4", "w-7/8", "w-full"];

const sgd = (m: { amount: string } | undefined) => (m ? `S$${formatAmount(m.amount)}` : "");

// The monitor's statuses, in plain words, as a chip.
const monitorWords: Record<Monitor["status"], [string, string, typeof CircleCheck]> = {
  below: ["Below the threshold", "fy-chip--good", CircleCheck],
  near: ["Near the threshold", "fy-chip--warning", TriangleAlert],
  liable: ["Must register", "fy-chip--serious", ClockAlert],
  registered: ["Registered", "fy-chip--good", CircleCheck],
  not_assessed: ["Not assessed", "fy-chip--neutral", CircleDashed],
};

function StatusChip({ m }: { m: Monitor }) {
  const [label, tone, icon] = monitorWords[m.status];
  return (
    <span className={`fy-chip ${tone}`} title={helpText(`gst-monitor.${m.status}`).tooltip}>
      <Icon of={icon} />
      {label}
    </span>
  );
}

// Test is one test's figure: a year's taxable supplies, with what it means against the threshold.
function Test({ title, t, help }: { title: string; t: YearTest; help: string }) {
  return (
    <div className="fy-stack fy-stack--1">
      <span className="fy-small fy-secondary" title={helpText(help).tooltip}>
        {title}
      </span>
      <span className="fy-h3 fy-num">{sgd(t.taxable_supplies)}</span>
      <span className="fy-small fy-muted">
        {date(t.from)} to {date(t.to)}, {t.entries} entr{t.entries === 1 ? "y" : "ies"}
      </span>
      {t.exceeded && (
        <span className="fy-chip fy-chip--serious" style={{ alignSelf: "flex-start" }}>
          <Icon of={ClockAlert} />
          More than the threshold: apply by {date(t.apply_by)}
        </span>
      )}
      {t.unmatched > 0 && (
        <span className="fy-small">
          {t.unmatched} document{t.unmatched === 1 ? "" : "s"} could not be read line by line and {t.unmatched === 1 ? "is" : "are"} left out: see Checks.
        </span>
      )}
    </div>
  );
}

// ForecastForm drafts a forecast of the next 12 months (FR-TAX-05; the owner's decision of 7 Nov 2026: recorded with its
// evidence, as IRAS asks).
function ForecastForm({ onClose, onDone }: { onClose: () => void; onDone: (text: string) => void }) {
  const id = useId();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const [form, setForm] = useState({ forecast_on: "", amount: "", basis: "" });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [key] = useState(newKey);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const body: ForecastDraft = { forecast_on: form.forecast_on, value: { amount: form.amount.replace(/[,\s]/g, ""), currency: "SGD" }, basis: form.basis.trim() };
    const out = await guarded(confirm, () => api.POST("/v1/gst-forecasts", { params: { header: { "Idempotency-Key": key } }, body }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["gst-forecasts"] });
    onDone("The forecast is drafted. Attach what it rests on, then record it.");
    onClose();
  };
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()} dismissible={!busy} eyebrow="GST registration" title="Record a forecast"
      description="The taxable supplies you expect in the 12 months from the day of the forecast. IRAS asks for supporting documents, such as signed contracts or confirmed orders.">
      {problem && <Refusal problem={problem} title="The forecast was not saved" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-on`}>
            Day of the forecast
          </label>
          <input className="fy-input" id={`${id}-on`} type="date" value={form.forecast_on} onChange={(e) => setForm({ ...form, forecast_on: e.target.value })} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-value`}>
            Taxable supplies expected, S$
          </label>
          <input className="fy-input fy-num" id={`${id}-value`} inputMode="decimal" value={form.amount} placeholder="1200000.00"
            onChange={(e) => setForm({ ...form, amount: e.target.value })} aria-describedby={`${id}-value-help`} />
          <span className="fy-help" id={`${id}-value-help`}>
            Standard-rated and zero-rated sales, without exempt or out-of-scope sales or sales of the company's own assets
          </span>
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-basis`}>
            What it rests on
          </label>
          <textarea className="fy-textarea" id={`${id}-basis`} maxLength={1000} value={form.basis} onChange={(e) => setForm({ ...form, basis: e.target.value })} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy}>
            Save the forecast
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// GstReturns is Compliance › GST (docs/design/screens GstReturns; FR-TAX-05, C.1 #37, design D4 and D12, Expert): the
// registration monitor's two tests and the year so far against the sg-gst pack's threshold, the days to apply by and to be
// registered from when a test is met, and the forecasts with their evidence. The return periods come with registration.
export function GstReturns() {
  const { session } = useSignedIn();
  const confirm = useConfirm();
  const queries = useQueryClient();
  const mayPrepare = (session.roles ?? []).some((r) => preparers.includes(r));
  const m = useQuery({ queryKey: ["gst-forecasts", "monitor"], queryFn: monitor });
  const all = useQuery({ queryKey: ["gst-forecasts", "list"], queryFn: forecasts });
  const [drafting, setDrafting] = useState(false);
  const [status, setStatus] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const draft = all.data?.find((f) => f.status === "draft") ?? null;
  const recorded = (all.data ?? []).filter((f) => f.status !== "draft");
  const draftH = helpText("gst-forecast.draft");
  const recordH = helpText("gst-forecast.record");
  const record = async () => {
    if (!draft) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/gst-forecasts/{forecast_id}/record", { params: { path: { forecast_id: draft.id }, header: { "If-Match": `"${draft.version}-draft"`, "Idempotency-Key": newKey() } } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["gst-forecasts"] });
    setStatus(`Recorded the forecast of ${date(out.data.forecast_on)}: the prospective test reads it.`);
  };
  const x = m.data;
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Compliance</span>
          <h1 className="fy-h1" title={helpText("compliance.gst").tooltip}>
            GST
          </h1>
          <p className="fy-secondary">Whether the company must register for GST, tested against IRAS's threshold, with the forecasts it rests on.</p>
        </div>
        {mayPrepare && !draft && x?.status !== "registered" && (
          <div className="fy-pagehead__actions">
            <Button variant="primary" onClick={() => setDrafting(true)} title={draftH.tooltip}>
              <Icon of={FileSignature} />
              {draftH.label}
            </Button>
          </div>
        )}
      </div>
      <p className="fy-small" role="status">
        {status}
      </p>
      {problem && <Refusal problem={problem} title="The forecast was not recorded" />}
      {m.isError && <Refusal problem={problemOf(m.error)} title="The monitor could not be shown" />}
      {m.isPending && <p className="fy-secondary">Loading the monitor…</p>}
      {x && (
        <section className="fy-card" aria-labelledby="monitor-title">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="monitor-title" title={helpText("gst.monitor").tooltip}>
              Registration monitor
            </h2>
            <StatusChip m={x} />
          </div>
          <div className="fy-card__body fy-stack fy-stack--4">
            {(x.status === "registered" || x.status === "not_assessed") && <p className="fy-small">{x.reason}</p>}
            {x.threshold && x.this_year && x.last_year && (
              <>
                <p className="fy-small fy-secondary">
                  IRAS asks a business to register when its taxable supplies are more than {sgd(x.threshold)} in a calendar year, or are expected to be in the next 12
                  months. The warning is at {x.warning_percent}% of it.
                </p>
                <div className="fy-grid fy-grid--3">
                  <Test title={`Last calendar year, ${x.last_year.year}`} t={x.last_year} help="gst-monitor.last-year" />
                  <div className="fy-stack fy-stack--1">
                    <Test title={`This year so far, ${x.this_year.year}`} t={x.this_year} help="gst-monitor.this-year" />
                    <div className={`fy-progress ${x.status === "below" ? "fy-progress--good" : "fy-progress--warning"}`} role="progressbar"
                      aria-label="This year so far, against the threshold" aria-valuemin={0} aria-valuemax={100}
                      aria-valuenow={Math.min(100, Number(x.share ?? "0"))} aria-valuetext={`${x.share}% of the threshold`}>
                      <span className={eighths[Math.min(8, Math.round(Number(x.share ?? "0") / 12.5))]} />
                    </div>
                    <span className="fy-small fy-num">{x.share}% of the threshold</span>
                  </div>
                  <div className="fy-stack fy-stack--1">
                    <span className="fy-small fy-secondary" title={helpText("gst-monitor.forecast").tooltip}>
                      The forecast of the next 12 months
                    </span>
                    {x.forecast ? (
                      <>
                        <span className="fy-h3 fy-num">{sgd(x.forecast.forecast.value)}</span>
                        <span className="fy-small fy-muted">
                          {date(x.forecast.forecast.forecast_on)} to {date(x.forecast.forecast.until)}
                        </span>
                        {x.forecast.exceeded && (
                          <span className="fy-chip fy-chip--serious" style={{ alignSelf: "flex-start" }}>
                            <Icon of={ClockAlert} />
                            More than the threshold: apply by {date(x.forecast.apply_by)}
                          </span>
                        )}
                      </>
                    ) : (
                      <span className="fy-small fy-muted">None recorded for today</span>
                    )}
                  </div>
                </div>
                {x.status === "liable" && (
                  <div className="fy-banner fy-banner--warning" role="status">
                    <Icon of={ClockAlert} size={20} />
                    <div className="fy-banner__text">
                      <strong>Apply to register for GST with IRAS</strong>
                      <span className="fy-small">
                        {[x.last_year, x.this_year]
                          .filter((t) => t.exceeded)
                          .map((t) => `Taxable supplies of ${t.year}: apply by ${date(t.apply_by)}, registered from ${date(t.registered_from)}.`)
                          .join(" ")}
                        {x.forecast?.exceeded ? ` The forecast: apply by ${date(x.forecast.apply_by)}, registered from ${date(x.forecast.registered_from)}.` : ""} Then
                        record the registration in Settings › Organisation.
                      </span>
                    </div>
                  </div>
                )}
              </>
            )}
            {x.status !== "registered" && (
              <p className="fy-small fy-muted">
                <Icon of={Info} /> While the company is not registered, GST that overseas suppliers charge is part of the cost.
              </p>
            )}
          </div>
        </section>
      )}
      {draft && (
        <section className="fy-card" aria-labelledby="forecast-draft-title">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="forecast-draft-title">
              Draft: forecast of {date(draft.forecast_on)}
            </h2>
            {mayPrepare && (
              <Button size="sm" onClick={() => void record()} disabled={busy || draft.evidence === 0} title={recordH.tooltip}>
                <Icon of={Stamp} />
                {recordH.label}
              </Button>
            )}
          </div>
          <div className="fy-card__body fy-stack fy-stack--3">
            <p className="fy-small">
              {sgd(draft.value)} expected from {date(draft.forecast_on)} to {date(draft.until)}. {draft.basis}
            </p>
            {draft.checks.map((c) => (
              <p key={c.code} className="fy-small fy-secondary">
                {c.message}. {c.fix}.
              </p>
            ))}
            <EvidencePanel record={{ kind: "gst-forecast", id: draft.id, etag: `"${draft.version}-draft"` }} draft mayAttach={mayPrepare}
              onChanged={(t) => {
                setStatus(t);
                void queries.invalidateQueries({ queryKey: ["gst-forecasts"] });
              }} />
          </div>
        </section>
      )}
      {recorded.length > 0 && (
        <section className="fy-card" aria-labelledby="forecasts-title">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="forecasts-title">
              Forecasts
            </h2>
          </div>
          <ul className="fy-list" aria-labelledby="forecasts-title">
            {recorded.map((f) => (
              <li key={f.id} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title">
                    {sgd(f.value)} from {date(f.forecast_on)}
                  </span>
                  <span className="fy-list__meta whitespace-normal">
                    {f.status === "recorded" ? "In force" : "Superseded"}
                    {f.recorded_by ? `, recorded by ${f.recorded_by.name}` : ""}
                    {f.recorded_at ? ` on ${dateTime(f.recorded_at)}` : ""}. {f.basis}
                  </span>
                </span>
              </li>
            ))}
          </ul>
        </section>
      )}
      {drafting && <ForecastForm onClose={() => setDrafting(false)} onDone={setStatus} />}
    </div>
  );
}
