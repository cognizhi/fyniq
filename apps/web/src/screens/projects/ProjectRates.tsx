import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { hours } from "./data";

const basisWords: Record<string, string> = {
  person: "each person's own rate",
  role_average: "the average rate of their role (three people or more)",
  standard: "the standard rate",
  mixed: "more than one rate",
  none: "no rate you may use: hours only",
};

function monthStart(): { from: string; to: string } {
  const d = new Date();
  const from = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), 1));
  const to = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 0));
  return { from: from.toISOString().slice(0, 10), to: to.toISOString().slice(0, 10) };
}

// ProjectRates are a project's billing rates and its approved time at cost this month (FR-PRJ-06, FR-PRJ-12): the cost is the
// server's, at the viewer's tier, with its basis said, and a person's own cost only for those who may see it.
export function ProjectRates({ project, billed, may }: { project: string; billed: boolean; may: boolean }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const [adding, setAdding] = useState(false);
  const [role, setRole] = useState("");
  const [from, setFrom] = useState("");
  const [rate, setRate] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const month = monthStart();
  const rates = useQuery({
    queryKey: ["billing-rates", project],
    queryFn: async () => {
      const r = await api.GET("/v1/projects/{project_id}/billing-rates", { params: { path: { project_id: project }, query: { page_size: 500 } } });
      if (r.error) throw r.error;
      return r.data.items;
    },
  });
  const cost = useQuery({
    queryKey: ["time-cost", project, month.from],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/projects/{project_id}/time-cost", { params: { path: { project_id: project }, query: month } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const h = helpText("rates.billing");
  const t = helpText("rates.time-cost");
  const close = () => {
    setAdding(false);
    setRole("");
    setFrom("");
    setRate("");
    setErrors({});
    setProblem(null);
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const errs: Record<string, string> = {};
    if (!from) errs.from = "Enter the day the rate starts.";
    if (!/^\d+(\.\d{1,6})?$/.test(rate.trim())) errs.rate = "Write the price of an hour as a plain number, such as 150.00.";
    setErrors(errs);
    if (Object.keys(errs).length) return;
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/billing-rates", { params: { header: { "Idempotency-Key": key } }, body: { project_id: project, role: role.trim() || undefined, effective_from: from, hourly_rate: rate.trim() } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["billing-rates", project] });
    close();
  };
  const c = cost.data;
  return (
    <>
      <section className="fy-card" aria-labelledby={`${id}-cost`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-cost`} title={t.tooltip}>
            {t.label}
          </h2>
        </div>
        {cost.isError && <Refusal problem={problemOf(cost.error)} title="The time cost could not be read" />}
        {c && (
          <div className="fy-card__body fy-stack fy-stack--2">
            <p>
              {hours(c.minutes)} approved this month, {hours(c.billable_minutes)} billable.{" "}
              {c.cost ? (
                <span className="fy-num">
                  Cost {c.currency} {c.cost}
                </span>
              ) : (
                <span className="fy-muted">No cost: you may see hours only.</span>
              )}
            </p>
            <p className="fy-small fy-muted">At {basisWords[c.rate_basis] ?? c.rate_basis}.</p>
            {c.people.length > 0 && (
              <table className="fy-table fy-table--compact" aria-label="Approved time by person">
                <thead>
                  <tr>
                    <th scope="col">Person</th>
                    <th scope="col" className="fy-num">
                      Hours
                    </th>
                    {c.people.some((p) => p.cost) && (
                      <th scope="col" className="fy-num">
                        Cost
                      </th>
                    )}
                  </tr>
                </thead>
                <tbody>
                  {c.people.map((p) => (
                    <tr key={p.user_id}>
                      <td>{p.person_name}</td>
                      <td className="fy-num">{hours(p.minutes)}</td>
                      {c.people.some((x) => x.cost) && <td className="fy-num">{p.cost ?? ""}</td>}
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        )}
      </section>
      {billed && (
        <section className="fy-card" aria-labelledby={`${id}-billing`}>
          <div className="fy-card__head fy-row--wrap gap-3">
            <h2 className="fy-h3" id={`${id}-billing`} title={h.tooltip}>
              {h.label}
            </h2>
            {may && (
              <Button variant="secondary" size="sm" onClick={() => setAdding(true)}>
                <Icon of={Plus} />
                Enter a billing rate
              </Button>
            )}
          </div>
          {rates.isError && <Refusal problem={problemOf(rates.error)} title="The billing rates could not be listed" />}
          {rates.data && rates.data.length === 0 && <p className="fy-card__body fy-small fy-secondary">No billing rates yet. {may ? "Enter the price of an hour for the project, a role or a person." : ""}</p>}
          {rates.data && rates.data.length > 0 && (
            <table className="fy-table fy-table--compact" aria-label="Billing rates">
              <thead>
                <tr>
                  <th scope="col">For</th>
                  <th scope="col">From</th>
                  <th scope="col" className="fy-num">
                    Price of an hour
                  </th>
                </tr>
              </thead>
              <tbody>
                {rates.data.map((r) => (
                  <tr key={r.id}>
                    <td>{[r.customer_id ? "The customer's projects" : "This project", r.role ? `role ${r.role}` : r.user_id ? "one person" : "everyone"].join(", ")}</td>
                    <td>{date(r.effective_from)}</td>
                    <td className="fy-num">
                      {r.currency} {r.hourly_rate}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>
      )}
      <Dialog open={adding} onOpenChange={(o) => !o && close()} dismissible={!busy} eyebrow="Billing rates" title="Enter a billing rate" description="In the project's currency. A rate never changes: a new one starts from its day.">
        {problem && <Refusal problem={problem} title="The rate was not saved" />}
        <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-role`}>
              Role (leave empty for everyone on the project)
            </label>
            <input className="fy-input" id={`${id}-role`} value={role} maxLength={80} onChange={(e) => setRole(e.target.value)} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-from`}>
              Starts
            </label>
            <input className="fy-input" id={`${id}-from`} type="date" value={from} onChange={(e) => setFrom(e.target.value)} aria-invalid={errors.from ? true : undefined} aria-describedby={errors.from ? `${id}-from-error` : undefined} />
            {errors.from && (
              <span className="fy-error" id={`${id}-from-error`}>
                {errors.from}
              </span>
            )}
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-rate`}>
              Price of an hour
            </label>
            <input className="fy-input fy-num" id={`${id}-rate`} inputMode="decimal" value={rate} onChange={(e) => setRate(e.target.value)} aria-invalid={errors.rate ? true : undefined} aria-describedby={errors.rate ? `${id}-rate-error` : undefined} />
            {errors.rate && (
              <span className="fy-error" id={`${id}-rate-error`}>
                {errors.rate}
              </span>
            )}
          </div>
          <div className="fy-row justify-end gap-2">
            <Button variant="ghost" type="button" onClick={close} disabled={busy}>
              Cancel
            </Button>
            <Button variant="primary" type="submit" disabled={busy}>
              Save the rate
            </Button>
          </div>
        </form>
      </Dialog>
    </>
  );
}
