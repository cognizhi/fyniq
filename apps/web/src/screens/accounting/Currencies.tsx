import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, Coins, FileUp, Plus, Search, TriangleAlert, Undo2 } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date, timeZone } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { who } from "./Journals";
import { Revaluation } from "./Revaluation";

type Rate = components["schemas"]["FXRate"];
type Entry = components["schemas"]["FXRateEntry"];
type Form = NonNullable<Entry["quote_form"]>;

// The roles that hold C on "Period close / reopen" (Appendix D), who enter and withdraw rates; the API decides.
const enterers = ["accountant", "controller"];

// today is the organisation's date, as the date picker's default and the feed's window end.
function today(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone }).format(new Date());
}

function daysBefore(iso: string, n: number): string {
  const d = new Date(`${iso}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() - n);
  return d.toISOString().slice(0, 10);
}

// monthName is "October 2026" for a month's first day.
function monthName(iso: string): string {
  return new Intl.DateTimeFormat("en-SG", { month: "long", year: "numeric", timeZone: "UTC" }).format(new Date(`${iso}T00:00:00Z`));
}

// forms are the ways a rate is quoted (RND-RATE), in words for the base and the currency.
function forms(base: string, cur: string): [Form, string][] {
  const c = cur || "the currency";
  return [
    ["base-per-unit", `${base} for 1 ${c}`],
    ["base-per-100", `${base} for 100 ${c}`],
    ["foreign-per-base", `${c} for 1 ${base}`],
  ];
}

function quoted(r: Rate, base: string): string {
  const form = forms(base, r.currency).find(([f]) => f === (r.quote_form ?? "base-per-unit"));
  return `${r.quote} ${form ? form[1] : ""}`;
}

// ImportRow is one row of a rates file, or why it cannot be read.
export interface ImportRow {
  line: number;
  entry?: Entry;
  problem?: string;
}

// parseRates reads a rates file: a header naming currency, date and quote, and optionally note, in any order, then a
// rate a row. Every row becomes an entry of kind and form, or says which line is wrong and why. The page never converts
// a rate: the API stores it under RND-RATE.
export function parseRates(text: string, kind: Entry["kind"], form: Form): { rows: ImportRow[]; problem?: string } {
  const lines = text.split(/\r?\n/).map((l, i) => ({ n: i + 1, cells: l.split(",").map((c) => c.trim().replace(/^"(.*)"$/, "$1")) }));
  const filled = lines.filter((l) => l.cells.some((c) => c !== ""));
  if (filled.length === 0) return { rows: [], problem: "The file is empty." };
  const header = (filled[0]?.cells ?? []).map((c) => c.toLowerCase());
  const at = (name: string) => header.indexOf(name);
  if (at("currency") < 0 || at("date") < 0 || at("quote") < 0) return { rows: [], problem: "The first line must name the columns currency, date and quote (and note, if you keep one)." };
  const rows = filled.slice(1).map((l): ImportRow => {
    const cell = (name: string) => (at(name) >= 0 ? (l.cells[at(name)] ?? "") : "");
    const currency = cell("currency").toUpperCase();
    const on = cell("date");
    const quote = cell("quote");
    if (!/^[A-Z]{3}$/.test(currency)) return { line: l.n, problem: `"${cell("currency")}" is not a currency code, such as USD.` };
    if (!/^\d{4}-\d{2}-\d{2}$/.test(on)) return { line: l.n, problem: `"${on}" is not a date written as 2026-10-16.` };
    if (!/^(0|[1-9]\d{0,9})(\.\d{1,10})?$/.test(quote)) return { line: l.n, problem: `"${quote}" is not a rate, such as 1.2953.` };
    const note = cell("note");
    return { line: l.n, entry: { kind, currency, on: kind === "month" ? `${on.slice(0, 7)}-01` : on, quote, quote_form: form, note: note || undefined } };
  });
  return rows.length === 0 ? { rows, problem: "The file has no rates under its first line." } : { rows };
}

// AddRate enters one rate: for a day, which overrides the daily feed's and replaces the organisation's earlier rate for
// that day, or for a month of the monthly table (FR-GL-05, ENG-40).
function AddRate({ base, initial, onClose, onDone }: { base: string; initial?: { currency: string; on: string }; onClose: () => void; onDone: () => void }) {
  const confirm = useConfirm();
  const id = useId();
  const h = helpText("fx.add-rate");
  const [kind, setKind] = useState<Entry["kind"]>("day");
  const [currency, setCurrency] = useState(initial?.currency ?? "");
  const [on, setOn] = useState(initial?.on ?? today());
  const [month, setMonth] = useState(today().slice(0, 7));
  const [quote, setQuote] = useState("");
  const [form, setForm] = useState<Form>("base-per-unit");
  const [note, setNote] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const cur = currency.trim().toUpperCase();
  const send = async () => {
    const e: Record<string, string> = {};
    if (!/^[A-Z]{3}$/.test(cur)) e.currency = "Enter a currency code, such as USD.";
    else if (cur === base) e.currency = `${base} is the base currency: it takes rate 1.`;
    if (kind === "day" && !on) e.on = "Enter the day the rate is for.";
    if (kind === "month" && !month) e.on = "Enter the month the rate is for.";
    if (!/^(0|[1-9]\d{0,9})(\.\d{1,10})?$/.test(quote.trim())) e.quote = "Enter the rate as a number with up to 10 decimal places, such as 1.2953.";
    setErrors(e);
    if (Object.keys(e).length > 0) return;
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/fx/rates", {
        params: { header: { "Idempotency-Key": key } },
        body: { kind, currency: cur, on: kind === "day" ? on : `${month}-01`, quote: quote.trim(), quote_form: form, note: note.trim() || undefined },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone();
  };
  const field = (name: string) => (errors[name] ? { "aria-invalid": true as const, "aria-describedby": `${id}-${name}-error` } : {});
  const error = (name: string) =>
    errors[name] ? (
      <span className="fy-error" id={`${id}-${name}-error`}>
        {errors[name]}
      </span>
    ) : null;
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title="Add a rate"
      description={`${h.tooltip} A rate is never changed: withdraw it and add another.`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void send()} disabled={busy}>
            <Icon of={Plus} />
            {h.label}
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The rate was not added" />}
      <fieldset className="fy-field">
        <legend className="fy-label">For</legend>
        <div className="fy-row fy-row--wrap gap-3">
          <label className="fy-row gap-2">
            <input type="radio" name={`${id}-kind`} checked={kind === "day"} onChange={() => setKind("day")} />A day: overrides the daily feed
          </label>
          <label className="fy-row gap-2">
            <input type="radio" name={`${id}-kind`} checked={kind === "month"} onChange={() => setKind("month")} />A month: the monthly table
          </label>
        </div>
      </fieldset>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-currency`}>
          Currency
        </label>
        <input className="fy-input fy-mono" id={`${id}-currency`} value={currency} maxLength={3} onChange={(e) => setCurrency(e.target.value.toUpperCase())} {...field("currency")} />
        {error("currency")}
      </div>
      {kind === "day" ? (
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-on`}>
            Day
          </label>
          <input className="fy-input" id={`${id}-on`} type="date" value={on} onChange={(e) => setOn(e.target.value)} {...field("on")} />
          {error("on")}
        </div>
      ) : (
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-month`}>
            Month
          </label>
          <input className="fy-input" id={`${id}-month`} type="month" value={month} onChange={(e) => setMonth(e.target.value)} {...field("on")} />
          {error("on")}
        </div>
      )}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-quote`}>
          Rate
        </label>
        <div className="fy-row fy-row--wrap gap-2">
          <input className="fy-input fy-num max-w-40" id={`${id}-quote`} inputMode="decimal" value={quote} onChange={(e) => setQuote(e.target.value)} {...field("quote")} />
          <label className="fy-sr" htmlFor={`${id}-form`}>
            Quoted as
          </label>
          <select className="fy-select max-w-56" id={`${id}-form`} value={form} onChange={(e) => setForm(e.target.value as Form)}>
            {forms(base, cur).map(([f, words]) => (
              <option key={f} value={f}>
                {words}
              </option>
            ))}
          </select>
        </div>
        {error("quote")}
        <span className="fy-small fy-muted">Fyniq keeps {base} for 1 unit, to 10 decimal places, and keeps your quote beside it.</span>
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-note`}>
          Where it came from (optional)
        </label>
        <input className="fy-input" id={`${id}-note`} value={note} maxLength={200} placeholder="Such as the bank's board rate" onChange={(e) => setNote(e.target.value)} />
      </div>
    </Dialog>
  );
}

// ImportRates enters the rates of a file, each on its own: a row the API refuses takes no other row with it, and the
// dialog lists every refusal with its line (FR-INT-11).
function ImportRates({ base, onClose, onDone }: { base: string; onClose: () => void; onDone: () => void }) {
  const confirm = useConfirm();
  const id = useId();
  const h = helpText("fx.import-rates");
  const [kind, setKind] = useState<Entry["kind"]>("day");
  const [form, setForm] = useState<Form>("base-per-unit");
  const [text, setText] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [result, setResult] = useState<{ saved: number; refused: { line: number; detail: string }[] } | null>(null);
  const parsed = text === null ? null : parseRates(text, kind, form);
  const ready = parsed?.rows.filter((r) => r.entry) ?? [];
  const unread = parsed?.rows.filter((r) => r.problem) ?? [];
  const send = async () => {
    if (ready.length === 0) return;
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () => api.POST("/v1/fx/rates:bulk", { params: { header: { "Idempotency-Key": key } }, body: { items: ready.map((r) => r.entry!) } }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setResult({
      saved: out.data.saved,
      refused: (out.data.results ?? []).filter((r) => r.problem).map((r) => ({ line: ready[r.index]?.line ?? 0, detail: r.problem?.errors?.[0]?.message ?? r.problem?.detail ?? "Refused" })),
    });
    onDone();
  };
  return (
    <Dialog
      open
      wide
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title="Import rates from a file"
      description={h.tooltip}
      footer={
        result ? (
          <Button variant="primary" onClick={onClose}>
            Done
          </Button>
        ) : (
          <>
            <Button variant="ghost" onClick={onClose} disabled={busy}>
              Cancel
            </Button>
            <Button variant="primary" onClick={() => void send()} disabled={busy || ready.length === 0}>
              <Icon of={FileUp} />
              {ready.length > 0 ? `Import ${ready.length} rate${ready.length === 1 ? "" : "s"}` : h.label}
            </Button>
          </>
        )
      }
    >
      {problem && <Refusal problem={problem} title="The rates were not imported" />}
      {result ? (
        <div className="fy-stack fy-stack--2" role="status">
          <span className="fy-chip fy-chip--good w-fit">
            <Icon of={CircleCheck} />
            {result.saved} imported
          </span>
          {result.refused.length > 0 && (
            <>
              <p className="fy-small">These lines were not imported:</p>
              <ul className="fy-small">
                {result.refused.map((r) => (
                  <li key={r.line}>
                    Line {r.line}: {r.detail}
                  </li>
                ))}
              </ul>
            </>
          )}
        </div>
      ) : (
        <>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-file`}>
              Rates file (CSV)
            </label>
            <input
              className="fy-input"
              id={`${id}-file`}
              type="file"
              accept=".csv,text/csv"
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (!f) return;
                setName(f.name);
                void f.text().then(setText);
              }}
            />
            <span className="fy-small fy-muted">The first line names the columns currency, date and quote, and note if you keep one, such as: currency,date,quote. Dates are written 2026-10-16.</span>
          </div>
          <div className="fy-row fy-row--wrap gap-3">
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-kind`}>
                Each rate is for
              </label>
              <select className="fy-select" id={`${id}-kind`} value={kind} onChange={(e) => setKind(e.target.value as Entry["kind"])}>
                <option value="day">Its day: overrides the daily feed</option>
                <option value="month">Its month: the monthly table</option>
              </select>
            </div>
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-form`}>
                Quoted as
              </label>
              <select className="fy-select" id={`${id}-form`} value={form} onChange={(e) => setForm(e.target.value as Form)}>
                {(
                  [
                    ["base-per-unit", `${base} for 1 unit of the currency`],
                    ["base-per-100", `${base} for 100 units of the currency`],
                    ["foreign-per-base", `Units of the currency for 1 ${base}`],
                  ] as [Form, string][]
                ).map(([f, words]) => (
                  <option key={f} value={f}>
                    {words}
                  </option>
                ))}
              </select>
            </div>
          </div>
          {parsed?.problem && <p className="fy-error">{parsed.problem}</p>}
          {parsed && !parsed.problem && (
            <p className="fy-small" role="status">
              {name}: {ready.length} rate{ready.length === 1 ? "" : "s"} to import
              {unread.length > 0 ? `, ${unread.length} line${unread.length === 1 ? "" : "s"} that cannot be read` : ""}.
            </p>
          )}
          {unread.length > 0 && (
            <ul className="fy-small fy-error">
              {unread.map((r) => (
                <li key={r.line}>
                  Line {r.line}: {r.problem}
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </Dialog>
  );
}

// Withdraw withdraws a rate with a reason: the cascade no longer finds it, and it stays listed (FR-GL-05).
function Withdraw({ rate, base, onClose, onDone }: { rate: Rate; base: string; onClose: () => void; onDone: () => void }) {
  const confirm = useConfirm();
  const id = useId();
  const h = helpText("fx.withdraw-rate");
  const [reason, setReason] = useState("");
  const [missing, setMissing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const send = async () => {
    if (!reason.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/fx/rates/{rate_id}/withdraw", {
        params: { path: { rate_id: rate.id }, header: { "Idempotency-Key": key, "If-Match": `"${rate.version}"` } },
        body: { reason: reason.trim() },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone();
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={`Withdraw the ${rate.currency} rate for ${rate.kind === "month" ? monthName(rate.on) : date(rate.on)}`}
      description={`${rate.rate} ${base} for 1 ${rate.currency}. ${h.tooltip}`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="secondary" onClick={() => void send()} disabled={busy}>
            <Icon of={Undo2} />
            {h.label}
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The rate was not withdrawn" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-reason`}>
          Why
        </label>
        <textarea
          className="fy-textarea"
          id={`${id}-reason`}
          value={reason}
          maxLength={500}
          onChange={(e) => {
            setReason(e.target.value);
            setMissing(false);
          }}
          aria-invalid={missing || undefined}
          aria-describedby={missing ? `${id}-reason-error` : undefined}
        />
        {missing && (
          <span className="fy-error" id={`${id}-reason-error`}>
            Say why the rate is wrong or no longer used.
          </span>
        )}
      </div>
    </Dialog>
  );
}

// FindRate asks the cascade what rate a posting on a date takes, and where it comes from (ENG-40).
function FindRate({ base }: { base: string }) {
  const id = useId();
  const h = helpText("fx.find-rate");
  const [currency, setCurrency] = useState("");
  const [on, setOn] = useState(today());
  const [asked, setAsked] = useState<{ currency: string; on: string } | null>(null);
  const found = useQuery({
    queryKey: ["fx-rate", asked?.currency, asked?.on],
    enabled: asked !== null,
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/fx/rate", { params: { query: { currency: asked!.currency, date: asked!.on } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  return (
    <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-find`}>
      <h2 className="fy-h3" id={`${id}-find`}>
        {h.label}
      </h2>
      <p className="fy-small fy-muted">{h.tooltip}</p>
      <form
        className="fy-row fy-row--wrap items-end gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          const c = currency.trim().toUpperCase();
          if (/^[A-Z]{3}$/.test(c) && on) setAsked({ currency: c, on });
        }}
      >
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-currency`}>
            Currency
          </label>
          <input className="fy-input fy-mono max-w-24" id={`${id}-currency`} value={currency} maxLength={3} onChange={(e) => setCurrency(e.target.value.toUpperCase())} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-on`}>
            Date
          </label>
          <input className="fy-input" id={`${id}-on`} type="date" value={on} onChange={(e) => setOn(e.target.value)} />
        </div>
        <Button variant="secondary" type="submit">
          <Icon of={Search} />
          Find
        </Button>
      </form>
      <div aria-live="polite">
        {found.isError && <Refusal problem={problemOf(found.error)} title={`No ${asked?.currency ?? ""} rate`} />}
        {found.data && (
          <p className="fy-row fy-row--wrap gap-2">
            <span className="fy-strong fy-num">
              1 {found.data.currency} = {found.data.rate} {base}
            </span>
            <span className="fy-small fy-secondary">
              on {date(found.data.date)}, from the {found.data.source}
            </span>
            {found.data.entries > 1 && (
              <span className="fy-chip fy-chip--warning">
                <Icon of={TriangleAlert} />
                Average of {found.data.entries} rates
              </span>
            )}
          </p>
        )}
      </div>
    </section>
  );
}

// Currencies is Accounting › Currencies (docs/design/screens Currencies, fx-revaluation; FR-GL-05, ENG-40; PRD-001 rev
// 0.70): what rate a posting takes and where from, the organisation's own rates, and the daily feed's last fortnight.
// The month-end revaluation joins it with its task. Every rate is the server's, stored under RND-RATE: the page shows
// rates as the API gives them and never converts one.
export function Currencies() {
  const { session, organisation } = useSignedIn();
  const base = organisation?.base_currency ?? "SGD";
  const queries = useQueryClient();
  const id = useId();
  const mayEnter = (session.roles ?? []).some((r) => enterers.includes(r));
  const [filter, setFilter] = useState("");
  const [withdrawn, setWithdrawn] = useState(false);
  const [adding, setAdding] = useState<false | { currency: string; on: string } | true>(false);
  const [tab, setTab] = useState<"rates" | "revaluation">("rates");
  const [importing, setImporting] = useState(false);
  const [withdrawing, setWithdrawing] = useState<Rate | null>(null);
  const cur = /^[A-Z]{3}$/.test(filter.trim().toUpperCase()) ? filter.trim().toUpperCase() : undefined;
  const rates = useQuery({
    queryKey: ["fx-rates", cur, withdrawn],
    queryFn: async () => {
      const r = await api.GET("/v1/fx/rates", { params: { query: { currency: cur, withdrawn, page_size: 500 } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const until = today();
  const feed = useQuery({
    queryKey: ["fx-feed", until],
    queryFn: async () => {
      const r = await api.GET("/v1/fx/feed", { params: { query: { from: daysBefore(until, 13), to: until, page_size: 500 } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const changed = () => void queries.invalidateQueries({ predicate: (q) => ["fx-rates", "fx-rate", "fx-revaluation"].includes(String(q.queryKey[0])) });
  const add = helpText("fx.add-rate");
  const imp = helpText("fx.import-rates");
  const items = rates.data?.items ?? [];
  const dups = rates.data?.duplicates ?? [];
  const isDup = (r: Rate) => r.kind === "month" && !r.withdrawn && dups.some((d) => d.currency === r.currency && d.month === r.on);
  const feedItems = feed.data?.items ?? [];
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting">Accounting</Link>
          </nav>
          <h1 className="fy-h1">Exchange rates</h1>
          <p className="fy-secondary">
            Base currency {base}. A posting in another currency takes its document's own rate, else your rate for the day, else the daily feed's, else your monthly table.
          </p>
        </div>
        {mayEnter && (
          <div className="fy-pagehead__actions">
            <Button variant="secondary" onClick={() => setImporting(true)} title={imp.tooltip}>
              <Icon of={FileUp} />
              {imp.label}
            </Button>
            <Button variant="primary" onClick={() => setAdding(true)} title={add.tooltip}>
              <Icon of={Plus} />
              {add.label}
            </Button>
          </div>
        )}
      </div>
      <ReviewBanner screen="fx-revaluation" />
      <div className="fy-tabs" role="tablist" aria-label="Currency views">
        {(
          [
            ["rates", "Rates"],
            ["revaluation", "Revaluation"],
          ] as const
        ).map(([key, label]) => (
          <button key={key} type="button" role="tab" id={`${id}-tab-${key}`} aria-selected={tab === key} aria-controls={`${id}-panel-${key}`} onClick={() => setTab(key)}>
            {label}
          </button>
        ))}
      </div>
      {tab === "revaluation" ? (
        <div role="tabpanel" id={`${id}-panel-revaluation`} aria-labelledby={`${id}-tab-revaluation`}>
          <Revaluation today={until} mayRun={mayEnter} onAddRate={(currency, on) => setAdding({ currency, on })} />
        </div>
      ) : (
        <div role="tabpanel" id={`${id}-panel-rates`} aria-labelledby={`${id}-tab-rates`} className="fy-stack fy-stack--4">
      {dups.length > 0 && (
        <div className="fy-banner fy-banner--warning" role="status">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">{dups.length === 1 ? "A month has more than one rate" : `${dups.length} months have more than one rate`}</span>
            <span className="fy-small">
              {dups.map((d) => `${d.currency} in ${monthName(d.month)}`).join("; ")}: postings take the average, which never sets a GST amount. Withdraw all but one.
            </span>
          </div>
        </div>
      )}
      <FindRate base={base} />
      <section className="fy-card" aria-labelledby={`${id}-own`}>
        <div className="fy-card__head fy-row--wrap gap-3">
          <h2 className="fy-h3" id={`${id}-own`}>
            Your rates
          </h2>
          <div className="fy-row fy-row--wrap gap-3">
            <div className="fy-inputgroup max-w-40">
              <span className="fy-inputgroup__addon">
                <Icon of={Search} />
              </span>
              <label className="fy-sr" htmlFor={`${id}-filter`}>
                Only one currency
              </label>
              <input className="fy-input fy-mono" id={`${id}-filter`} placeholder="Currency" maxLength={3} value={filter} onChange={(e) => setFilter(e.target.value.toUpperCase())} />
            </div>
            <label className="fy-row gap-2 fy-small">
              <input type="checkbox" checked={withdrawn} onChange={(e) => setWithdrawn(e.target.checked)} />
              Show withdrawn rates
            </label>
          </div>
        </div>
        {rates.isError && <Refusal problem={problemOf(rates.error)} title="The rates could not be listed" />}
        {rates.data && items.length === 0 && (
          <div className="fy-empty fy-card--pad">
            <Icon of={Coins} size={24} />
            <h2 className="fy-h3">{cur ? `No ${cur} rates of your own` : "No rates of your own"}</h2>
            <p className="fy-secondary">The daily feed covers most days. Add a rate for a day the feed missed, or a monthly rate for postings with no rate of their own.</p>
          </div>
        )}
        {items.length > 0 && (
          <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
            <table className="fy-table fy-table--cards fy-table--compact">
              <thead>
                <tr>
                  <th scope="col">Currency</th>
                  <th scope="col">For</th>
                  <th scope="col" className="fy-num">
                    Rate ({base} for 1)
                  </th>
                  <th scope="col" className="fy-from-medium">
                    Entered as
                  </th>
                  <th scope="col" className="fy-from-expanded">
                    Entered by
                  </th>
                  <th scope="col">Status</th>
                </tr>
              </thead>
              <tbody>
                {items.map((r) => (
                  <tr key={r.id}>
                    <td data-card="title" className="fy-mono">
                      {r.currency}
                    </td>
                    <td data-card="meta">{r.kind === "month" ? `${monthName(r.on)} (monthly table)` : date(r.on)}</td>
                    <td data-card="amount" className="fy-num">
                      {r.rate}
                    </td>
                    <td data-card="meta" className="fy-from-medium">
                      {quoted(r, base)}
                      {r.note ? ` · ${r.note}` : ""}
                    </td>
                    <td data-card="hide" className="fy-from-expanded">
                      {who(r.entered_by)}
                    </td>
                    <td data-card="status">
                      <span className="fy-row fy-row--wrap gap-2">
                        {r.withdrawn ? (
                          <span className="fy-chip fy-chip--neutral" title={r.withdrawn_reason}>
                            <Icon of={Undo2} />
                            Withdrawn
                          </span>
                        ) : isDup(r) ? (
                          <span className="fy-chip fy-chip--warning">
                            <Icon of={TriangleAlert} />
                            Averaged with another
                          </span>
                        ) : (
                          <span className="fy-chip fy-chip--good">
                            <Icon of={CircleCheck} />
                            In use
                          </span>
                        )}
                        {mayEnter && !r.withdrawn && (
                          <Button variant="ghost" size="sm" onClick={() => setWithdrawing(r)} aria-label={`Withdraw the ${r.currency} rate for ${r.kind === "month" ? monthName(r.on) : date(r.on)}`}>
                            Withdraw
                          </Button>
                        )}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      <section className="fy-card" aria-labelledby={`${id}-feed`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-feed`}>
            Daily feed, last two weeks
          </h2>
          <span className="fy-small fy-muted">Business days only; a weekend posting takes the latest rate within 7 days</span>
        </div>
        {feed.isError && <Refusal problem={problemOf(feed.error)} title="The daily feed could not be listed" />}
        {feed.data && feedItems.length === 0 && (
          <p className="fy-card__body fy-secondary">The daily feed has no rates for these two weeks. Until it is connected, postings take your own rates.</p>
        )}
        {feedItems.length > 0 && (
          <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
            <table className="fy-table fy-table--cards fy-table--compact">
              <thead>
                <tr>
                  <th scope="col">Currency</th>
                  <th scope="col">Date</th>
                  <th scope="col" className="fy-num">
                    Rate ({base} for 1)
                  </th>
                  <th scope="col">Source</th>
                </tr>
              </thead>
              <tbody>
                {feedItems.map((r) => (
                  <tr key={`${r.currency} ${r.on}`}>
                    <td data-card="title" className="fy-mono">
                      {r.currency}
                    </td>
                    <td data-card="meta">{date(r.on)}</td>
                    <td data-card="amount" className="fy-num">
                      {r.rate}
                    </td>
                    <td data-card="meta">{r.feed}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
        </div>
      )}
      {adding && <AddRate base={base} initial={adding === true ? undefined : adding} onClose={() => setAdding(false)} onDone={() => (setAdding(false), changed())} />}
      {importing && <ImportRates base={base} onClose={() => setImporting(false)} onDone={changed} />}
      {withdrawing && <Withdraw rate={withdrawing} base={base} onClose={() => setWithdrawing(null)} onDone={() => (setWithdrawing(null), changed())} />}
    </div>
  );
}
