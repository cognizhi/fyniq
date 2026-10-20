import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CalendarPlus, CalendarRange, CircleCheck, Lock } from "lucide-react";
import { useState } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listPeriods, type Period, type PeriodPage } from "./data";

type Year = NonNullable<PeriodPage["next_year"]>;

// Who opens a financial year: C on "Period close / reopen" (Appendix D), the Accountant and the Controller.
const openers = ["accountant", "controller"];

const month = new Intl.DateTimeFormat("en-SG", { month: "long", year: "numeric", timeZone: "UTC" });

// monthOf names a period by its month: "October 2026"; a short first period says from which day.
export function monthOf(p: Period): string {
  const start = new Date(`${p.starts_on}T00:00:00Z`);
  const name = month.format(start);
  return start.getUTCDate() === 1 ? name : `${name}, from ${date(p.starts_on)}`;
}

function OpenYear({ year, onClose, onOpened }: { year: Year | null; onClose: () => void; onOpened: (n: number) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("period.open-year");
  const run = async () => {
    if (!year) return;
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () => api.POST("/v1/periods:open-year", { params: { header: { "Idempotency-Key": key } }, body: { starts_on: year.starts_on } }));
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["periods"] });
    onOpened(out.data.periods.length);
    onClose();
  };
  return (
    <Dialog
      open={year !== null}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={year ? `Open financial year ${year.number}?` : ""}
      description={
        year
          ? `${date(year.starts_on)} to ${date(year.ends_on)}: ${year.periods} monthly periods. Journals and documents dated in them can then post. Once a period exists, when the books start and how the years run no longer change.`
          : ""
      }
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={run} disabled={busy}>
            <Icon of={CalendarPlus} />
            {h.label}
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The year was not opened" />}
    </Dialog>
  );
}

// Periods is Accounting › Periods and close (docs/design/screens PeriodClose, its periods; FR-ORG-02, FR-GL-08): the
// accounting periods by financial year, each open or closed, and opening the next financial year. A posting's period
// is the month of its date, and nothing posts on a date no open period covers. Each month opens its close page.
export function Periods() {
  const { session } = useSignedIn();
  const q = useQuery({ queryKey: ["periods"], queryFn: listPeriods });
  const [opening, setOpening] = useState<Year | null>(null);
  const [status, setStatus] = useState("");
  const may = (session.roles ?? []).some((r) => openers.includes(r));
  const h = helpText("period.open-year");
  const periods = q.data?.items ?? [];
  const next = q.data?.next_year;
  const years = [...new Set(periods.map((p) => p.financial_year ?? 0))];

  return (
    <div className="fy-page fy-page--narrow fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/accounting">Accounting</Link>
          </nav>
          <h1 className="fy-h1">Periods and close</h1>
          <p className="fy-secondary">Each month is a period. A journal or document posts in the month of its date, while that month is open.</p>
        </div>
      </div>
      <ReviewBanner screen="period-close" />
      <p className="fy-small" role="status">
        {status}
      </p>
      {q.isError && <Refusal problem={problemOf(q.error)} title="The periods could not be listed" />}
      {q.isPending && <p className="fy-secondary">Loading the periods…</p>}
      {q.data && !next && periods.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={CalendarRange} size={24} />
          <h2 className="fy-h3">Set when the books start first</h2>
          <p className="fy-secondary">The financial years run from the day the books start. Enter it, and when the first year ends, in the organisation's profile.</p>
          <Link className="fy-btn fy-btn--primary" to="/settings/organisation">
            Go to Settings › Organisation
          </Link>
        </div>
      )}
      {next && (
        <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby="next-year">
          <h2 className="fy-h3" id="next-year">
            {periods.length === 0 ? "Open the first financial year" : `Next: financial year ${next.number}`}
          </h2>
          <p className="fy-secondary">
            {date(next.starts_on)} to {date(next.ends_on)}, {next.periods} periods.{" "}
            {next.may_open ? "Nothing can post on a date until its year is open." : "It can open once the year before it has started."}
          </p>
          {may ? (
            <div>
              <Button variant={periods.length === 0 ? "primary" : "secondary"} onClick={() => setOpening(next)} disabled={!next.may_open} title={h.tooltip}>
                <Icon of={CalendarPlus} />
                {h.label}
              </Button>
            </div>
          ) : (
            <p className="fy-small fy-secondary">An Accountant or a Controller opens the year.</p>
          )}
        </section>
      )}
      {years.map((y) => {
        const mine = periods.filter((p) => (p.financial_year ?? 0) === y);
        const hid = `year-${y}`;
        return (
          <section key={y} className="fy-card" aria-labelledby={hid}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={hid}>
                {y ? `Financial year ${y}` : "Periods"}
              </h2>
              <span className="fy-small fy-muted">
                {date(mine[0]?.starts_on)} to {date(mine[mine.length - 1]?.ends_on)}
              </span>
            </div>
            <div className="fy-tablewrap">
              <table className="fy-table fy-table--compact" aria-labelledby={hid}>
                <thead>
                  <tr>
                    <th scope="col">Month</th>
                    <th scope="col" className="fy-from-medium">
                      Dates
                    </th>
                    <th scope="col">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {mine.map((p) => (
                    <tr key={p.id}>
                      <td>
                        <Link to="/accounting/close/$periodId" params={{ periodId: p.id }}>
                          {monthOf(p)}
                        </Link>
                      </td>
                      <td className="fy-from-medium fy-secondary">
                        {date(p.starts_on)} to {date(p.ends_on)}
                      </td>
                      <td>
                        {p.status === "closed" ? (
                          <span className="fy-chip fy-chip--neutral">
                            <Icon of={Lock} />
                            Closed
                          </span>
                        ) : (
                          <span className="fy-chip fy-chip--good">
                            <Icon of={CircleCheck} />
                            Open
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        );
      })}
      <OpenYear year={opening} onClose={() => setOpening(null)} onOpened={(n) => setStatus(`The year is open: ${n} ${n === 1 ? "period" : "periods"}.`)} />
    </div>
  );
}
