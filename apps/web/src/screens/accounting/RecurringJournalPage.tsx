import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { CirclePause, CirclePlay, Eye, Save } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { date, dateTime, timeZone } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listAccounts, postable } from "./data";
import { Checks } from "./JournalPage";
import { CheckChip, preparers, who } from "./Journals";
import { checkLines, LineEditor, newLine, toLines, type Errors, type Line } from "./lines";
import { RecurringStatusChip, repeats, type Recurring } from "./RecurringJournals";

type Input = components["schemas"]["RecurringJournalInput"];

export interface RecurringForm {
  name: string;
  every_months: string; // "1", "3", "6" or "12"
  day_of_month: string;
  starts_on: string;
  ends_on: string; // empty: no end
  memo: string;
  reverses: boolean;
  lines: Line[];
}

// recurringFormOf is a saved recurring journal as the form shows it.
export function recurringFormOf(r: Recurring): RecurringForm {
  return {
    name: r.name,
    every_months: String(r.schedule.every_months),
    day_of_month: String(r.schedule.day_of_month),
    starts_on: r.schedule.starts_on,
    ends_on: r.schedule.ends_on ?? "",
    memo: r.memo ?? "",
    reverses: r.reverses,
    lines: toLines(r.lines ?? []),
  };
}

// checkRecurring turns what was typed into the recurring journal to send, or says what to fix under each field. The
// API checks the lines as the posting rules would and says so in the statuses.
export function checkRecurring(f: RecurringForm, base: string): { input?: Input; errors: Errors } {
  const errors: Errors = {};
  if (!f.name.trim()) errors.name = "Name it, such as Office rent accrual.";
  const day = Number(f.day_of_month);
  if (!Number.isInteger(day) || day < 1 || day > 31) errors.day_of_month = "Enter a day from 1 to 31.";
  if (!f.starts_on) errors.starts_on = "Enter the date it starts from.";
  if (f.ends_on && f.starts_on && f.ends_on < f.starts_on) errors.ends_on = "End it on or after its start, or leave the end empty.";
  const lines = checkLines(f.lines, base, errors);
  if (lines.length === 0) errors.lines = "Add a line.";
  if (Object.keys(errors).length > 0) return { errors };
  return {
    input: {
      name: f.name.trim(),
      schedule: { every_months: Number(f.every_months) as 1 | 3 | 6 | 12, day_of_month: day, starts_on: f.starts_on, ends_on: f.ends_on || undefined },
      memo: f.memo.trim() || undefined,
      lines,
      reverses: f.reverses || undefined,
    },
    errors,
  };
}

function today(): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
}

// firstOfMonth is the first day of the month of a date (YYYY-MM-DD).
function firstOfMonth(d: string): string {
  return `${d.slice(0, 8)}01`;
}

// RecurringJournalPage is a recurring journal (docs/design/screens Journals, JournalEditor's Repeat; FR-GL-02,
// FR-AUT-01; PRD-001 rev 0.58, rev 0.60): its name, schedule and lines. Fyniq makes the draft of each occurrence on its
// date; a person checks and posts it under the usual approvals. Whoever writes the lines does not approve the posting
// of the drafts made from them (SOD-3). Check shows the next draft's date and what would stop the drafts posting,
// without saving (FR-INT-14).
export function RecurringJournalPage() {
  const { recurringId } = useParams({ strict: false }) as { recurringId?: string };
  const isNew = !recurringId;
  const { session, organisation } = useSignedIn();
  const base = organisation?.base_currency ?? "SGD";
  const queries = useQueryClient();
  const navigate = useNavigate();
  const confirm = useConfirm();
  const id = useId();
  const mayPrepare = (session.roles ?? []).some((r) => preparers.includes(r));

  const accounts = useQuery({ queryKey: ["accounts"], queryFn: listAccounts });
  const one = useQuery({
    queryKey: ["recurring-journal", recurringId],
    enabled: !isNew,
    queryFn: async () => {
      const r = await api.GET("/v1/recurring-journals/{recurring_journal_id}", { params: { path: { recurring_journal_id: recurringId ?? "" } } });
      if (r.error) throw r.error;
      return { recurring: r.data, etag: etagOf(r.response) };
    },
  });
  const r = one.data?.recurring;
  const [form, setForm] = useState<RecurringForm | null>(null);
  const start = firstOfMonth(today());
  const loaded: RecurringForm | null = isNew
    ? { name: "", every_months: "1", day_of_month: "1", starts_on: start, ends_on: "", memo: "", reverses: false, lines: [newLine(base), newLine(base)] }
    : r
      ? recurringFormOf(r)
      : null;
  const f = form ?? loaded;
  const dirty = form !== null;
  const [errors, setErrors] = useState<Errors>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [failed, setFailed] = useState("");
  const [preview, setPreview] = useState<Recurring | null>(null);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState("");
  const byId = new Map((accounts.data ?? []).map((a) => [a.id, a]));
  const choices = postable(accounts.data ?? []);
  const saveHelp = helpText("recurring.save");
  const checkHelp = helpText("recurring.check");
  const reversesHelp = helpText("recurring.reverses");
  const pauseHelp = helpText("recurring.pause");
  const resumeHelp = helpText("recurring.resume");

  const change = (next: RecurringForm) => {
    setForm(next);
    setPreview(null);
  };
  const fieldError = (key: string) =>
    errors[key] ? (
      <span className="fy-error" id={`${id}-${key}-error`}>
        {errors[key]}
      </span>
    ) : null;
  const described = (key: string) => (errors[key] ? `${id}-${key}-error` : undefined);

  // send saves the recurring journal, or with dryRun checks it and saves nothing (Prefer: return=preview).
  const send = async (dryRun: boolean, e?: FormEvent) => {
    e?.preventDefault();
    if (!f) return;
    const { input, errors: errs } = checkRecurring(f, base);
    setErrors(errs);
    if (!input) return setStatus(`Fix the fields marked, then ${dryRun ? "check" : "save"} again.`);
    setBusy(true);
    setProblem(null);
    const prefer = dryRun ? { Prefer: "return=preview" } : {};
    const out = isNew
      ? await guarded(confirm, () => api.POST("/v1/recurring-journals", { params: { header: { "Idempotency-Key": newKey(), ...prefer } }, body: input }))
      : await guarded(confirm, () =>
          api.PUT("/v1/recurring-journals/{recurring_journal_id}", { params: { path: { recurring_journal_id: recurringId ?? "" }, header: { "If-Match": one.data?.etag ?? "", ...prefer } }, body: input }),
        );
    setBusy(false);
    setFailed(dryRun ? "The recurring journal could not be checked" : "The recurring journal was not saved");
    if (!out.data) {
      setPreview(null);
      return setProblem(problemOf(out.error));
    }
    if (dryRun) {
      setPreview(out.data);
      return setStatus(out.data.next_on ? `Its next draft would be made on ${date(out.data.next_on)}. Nothing was saved.` : "It would make no draft. Nothing was saved.");
    }
    await queries.invalidateQueries({ queryKey: ["recurring-journals"] });
    setForm(null);
    setPreview(null);
    if (isNew) return navigate({ to: "/accounting/journals/recurring/$recurringId", params: { recurringId: out.data.id } });
    await queries.invalidateQueries({ queryKey: ["recurring-journal", recurringId] });
    setStatus(out.data.next_on ? `Saved. Its next draft is made on ${date(out.data.next_on)}.` : "Saved.");
  };

  const pauseOrResume = async (to: "pause" | "resume") => {
    if (!one.data) return;
    setBusy(true);
    setProblem(null);
    const params = { path: { recurring_journal_id: one.data.recurring.id }, header: { "If-Match": one.data.etag, "Idempotency-Key": newKey() } };
    const out = await guarded(confirm, () => (to === "pause" ? api.POST("/v1/recurring-journals/{recurring_journal_id}/pause", { params }) : api.POST("/v1/recurring-journals/{recurring_journal_id}/resume", { params })));
    setBusy(false);
    setFailed(to === "pause" ? "The recurring journal was not paused" : "The recurring journal was not resumed");
    if (!out.data) return setProblem(problemOf(out.error));
    await Promise.all([queries.invalidateQueries({ queryKey: ["recurring-journals"] }), queries.invalidateQueries({ queryKey: ["recurring-journal", recurringId] })]);
    setStatus(to === "pause" ? "Paused: it makes no drafts until you resume it." : out.data.next_on ? `Resumed: its next draft is made on ${date(out.data.next_on)}.` : "Resumed.");
  };

  const actions = (
    <>
      <Button type="button" onClick={() => void send(true)} disabled={busy} title={checkHelp.tooltip}>
        <Icon of={Eye} />
        {checkHelp.label}
      </Button>
      {r && !dirty && r.status === "active" && (
        <Button type="button" onClick={() => void pauseOrResume("pause")} disabled={busy} title={pauseHelp.tooltip}>
          <Icon of={CirclePause} />
          {pauseHelp.label}
        </Button>
      )}
      {r && !dirty && r.status === "paused" && (
        <Button type="button" onClick={() => void pauseOrResume("resume")} disabled={busy} title={resumeHelp.tooltip}>
          <Icon of={CirclePlay} />
          {resumeHelp.label}
        </Button>
      )}
      <Button variant="primary" type="submit" form={`${id}-form`} disabled={busy || (!isNew && !dirty)} title={saveHelp.tooltip}>
        <Icon of={Save} />
        {saveHelp.label}
      </Button>
    </>
  );

  const shown = preview ?? r;
  const title = isNew ? "New recurring journal" : (r?.name ?? "Recurring journal");

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting">Accounting</Link>
            <span aria-hidden="true">›</span>
            <Link to="/accounting/journals/recurring">Recurring journals</Link>
          </nav>
          <div className="fy-row fy-row--wrap gap-2">
            <h1 className="fy-h1">{title}</h1>
            {r && <RecurringStatusChip status={r.status} />}
            {r && <CheckChip checks={r.checks.filter((c) => c.severity !== "ok")} />}
          </div>
          {r && (
            <p className="fy-secondary">
              {repeats(r.schedule)}
              {r.next_on ? ` · next draft on ${date(r.next_on)}` : " · no more drafts"}
              {r.edited_by ? ` · lines by ${who(r.edited_by)} on ${dateTime(r.edited_at)}` : ""}
            </p>
          )}
        </div>
        {mayPrepare && <div className="fy-pagehead__actions fy-from-medium">{actions}</div>}
      </div>
      <ReviewBanner screen="journals" />
      <p className="fy-small" role="status">
        {status}
      </p>
      {one.isError && <Refusal problem={problemOf(one.error)} title="The recurring journal could not be shown" />}
      {problem && <Refusal problem={problem} title={failed} />}
      {!isNew && one.isPending && <p className="fy-secondary">Loading the recurring journal…</p>}
      {shown && <Checks checks={shown.checks} heading={(n) => (n === 1 ? "1 thing to look at before its drafts can post" : `${n} things to look at before its drafts can post`)} />}
      {r?.last_journal_id && (
        <p className="fy-small">
          Its last draft, for {r.last_on ? date(r.last_on) : "its last date"}:{" "}
          <Link to="/accounting/journals/$journalId" params={{ journalId: r.last_journal_id }}>
            open the journal
          </Link>
          . Drafts wait for a person to post them, under the usual approvals; whoever wrote these lines does not approve them.
        </p>
      )}
      {isNew && !mayPrepare && <p className="fy-secondary">An Accountant or a Tax agent sets up recurring journals.</p>}

      {f && (
        <form id={`${id}-form`} className="fy-stack fy-stack--6" onSubmit={(e) => void send(false, e)} noValidate>
          <fieldset className="fy-stack fy-stack--6 m-0 min-w-0 border-0 p-0" disabled={!mayPrepare}>
            <section className="fy-card" aria-labelledby={`${id}-head`}>
              <div className="fy-card__head">
                <h2 className="fy-h3" id={`${id}-head`}>
                  Recurring journal
                </h2>
              </div>
              <div className="fy-card__body fy-form fy-form--2">
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-name`}>
                    Name
                  </label>
                  <input className="fy-input" id={`${id}-name`} value={f.name} maxLength={120} placeholder="Office rent accrual" onChange={(e) => change({ ...f, name: e.target.value })} aria-invalid={errors.name ? true : undefined} aria-describedby={described("name")} />
                  {fieldError("name")}
                </div>
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-memo`}>
                    Description of each draft
                  </label>
                  <input className="fy-input" id={`${id}-memo`} value={f.memo} maxLength={1000} placeholder="What each journal is for" onChange={(e) => change({ ...f, memo: e.target.value })} />
                </div>
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-every`}>
                    Repeats
                  </label>
                  <select className="fy-select" id={`${id}-every`} value={f.every_months} onChange={(e) => change({ ...f, every_months: e.target.value })}>
                    <option value="1">Every month</option>
                    <option value="3">Every 3 months</option>
                    <option value="6">Every 6 months</option>
                    <option value="12">Every year</option>
                  </select>
                </div>
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-day`}>
                    On day
                  </label>
                  <input className="fy-input fy-num" id={`${id}-day`} inputMode="numeric" value={f.day_of_month} onChange={(e) => change({ ...f, day_of_month: e.target.value })} aria-invalid={errors.day_of_month ? true : undefined} aria-describedby={described("day_of_month") ?? `${id}-day-hint`} />
                  <span className="fy-small fy-muted" id={`${id}-day-hint`}>
                    The 29th to the 31st fall on the last day of a shorter month.
                  </span>
                  {fieldError("day_of_month")}
                </div>
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-starts`}>
                    Starts on
                  </label>
                  <input className="fy-input" id={`${id}-starts`} type="date" value={f.starts_on} onChange={(e) => change({ ...f, starts_on: e.target.value })} aria-invalid={errors.starts_on ? true : undefined} aria-describedby={described("starts_on") ?? `${id}-starts-hint`} />
                  <span className="fy-small fy-muted" id={`${id}-starts-hint`}>
                    A start in the past makes the drafts of the dates already passed.
                  </span>
                  {fieldError("starts_on")}
                </div>
                <div className="fy-field">
                  <label className="fy-label" htmlFor={`${id}-ends`}>
                    Ends on (optional)
                  </label>
                  <input className="fy-input" id={`${id}-ends`} type="date" value={f.ends_on} onChange={(e) => change({ ...f, ends_on: e.target.value })} aria-invalid={errors.ends_on ? true : undefined} aria-describedby={described("ends_on")} />
                  {fieldError("ends_on")}
                </div>
                <div className="fy-field">
                  <label className="fy-row gap-2 fy-label">
                    <input type="checkbox" checked={f.reverses} onChange={(e) => change({ ...f, reverses: e.target.checked })} title={reversesHelp.tooltip} />
                    {reversesHelp.label}
                  </label>
                  <span className="fy-small fy-muted">On the first day of the next month, once posted: for an accrual.</span>
                </div>
              </div>
            </section>
            <LineEditor id={id} lines={f.lines} onChange={(lines) => change({ ...f, lines })} base={base} choices={choices} byId={byId} errors={errors} title="Lines of each draft" />
          </fieldset>
        </form>
      )}
      {mayPrepare && <div className="fy-actionbar fy-only-compact">{actions}</div>}
    </div>
  );
}
