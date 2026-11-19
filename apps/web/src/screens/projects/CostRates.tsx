import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRight, CircleDollarSign, Plus } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

// CostRates is Projects › Cost rates (FR-PRJ-06; the owner's decision of 19 Nov 2026): each person's cost of an hour and the
// organisation's standard rate, for the Owner and the Payroll administrator only. A person's rate is payroll data (FR-PAY-13):
// nobody else is sent one, and they see time at the role-average rate. A rate never changes: a new one starts from its day.
export function CostRates() {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const [adding, setAdding] = useState(false);
  const [who, setWho] = useState("");
  const [role, setRole] = useState("");
  const [from, setFrom] = useState("");
  const [rate, setRate] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const q = useQuery({
    queryKey: ["cost-rates"],
    queryFn: async () => {
      const r = await api.GET("/v1/cost-rates", { params: { query: { page_size: 500 } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const h = helpText("rates.cost");
  const close = () => {
    setAdding(false);
    setWho("");
    setRole("");
    setFrom("");
    setRate("");
    setErrors({});
    setProblem(null);
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const errs: Record<string, string> = {};
    if (who !== "standard" && who === "") errs.who = "Choose the person, or the standard rate.";
    if (who !== "standard" && who !== "" && !role.trim()) errs.role = "Name the role the rate is grouped under, such as Consultant.";
    if (!from) errs.from = "Enter the day the rate starts.";
    if (!/^\d+(\.\d{1,6})?$/.test(rate.trim())) errs.rate = "Write the cost of an hour as a plain number with at most 6 decimals, such as 62.50.";
    setErrors(errs);
    if (Object.keys(errs).length) return;
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/cost-rates", {
        params: { header: { "Idempotency-Key": key } },
        body: { user_id: who === "standard" ? undefined : who, role: who === "standard" ? undefined : role.trim(), effective_from: from, hourly_rate: rate.trim() },
      }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["cost-rates"] });
    close();
  };
  const field = (k: string) => (errors[k] ? { "aria-invalid": true as const, "aria-describedby": `${id}-${k}-error` } : {});
  const error = (k: string) =>
    errors[k] ? (
      <span className="fy-error" id={`${id}-${k}-error`}>
        {errors[k]}
      </span>
    ) : null;
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/projects">Projects</Link>
            <Icon of={ChevronRight} size={16} />
          </nav>
          <h1 className="fy-h1">Cost rates</h1>
          <p className="fy-secondary">What an hour of each person's time costs. Only you see these: everyone else sees time at the average of a role with at least three people, or the standard rate.</p>
        </div>
        <div className="fy-pagehead__actions">
          <Button variant="primary" onClick={() => setAdding(true)} title={h.tooltip}>
            <Icon of={Plus} />
            {h.label}
          </Button>
        </div>
      </div>
      {q.isError && <Refusal problem={problemOf(q.error)} title="The rates could not be listed" />}
      {q.data && (
        <section className="fy-card" aria-labelledby={`${id}-list`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-list`}>
              Rates
            </h2>
          </div>
          {q.data.items.length === 0 ? (
            <div className="fy-empty fy-card--pad">
              <Icon of={CircleDollarSign} size={24} />
              <h2 className="fy-h3">No cost rates yet</h2>
              <p className="fy-secondary">Enter each person's cost of an hour, and a standard rate for viewers who may not see a person's own. Until payroll fills them in, you type them.</p>
              <Button variant="secondary" size="sm" className="self-center" onClick={() => setAdding(true)}>
                <Icon of={Plus} />
                {h.label}
              </Button>
            </div>
          ) : (
            <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
              <table className="fy-table fy-table--compact" aria-label="Cost rates">
                <thead>
                  <tr>
                    <th scope="col">Person</th>
                    <th scope="col">Role</th>
                    <th scope="col">From</th>
                    <th scope="col" className="fy-num">
                      Cost of an hour
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {q.data.items.map((r) => (
                    <tr key={r.id}>
                      <td>{r.person_name ?? "Standard rate"}</td>
                      <td>{r.role}</td>
                      <td>{date(r.effective_from)}</td>
                      <td className="fy-num">
                        {r.currency} {r.hourly_rate}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}
      <Dialog open={adding} onOpenChange={(o) => !o && close()} dismissible={!busy} eyebrow="Cost rates" title="Enter a cost rate" description="A rate never changes: a new one starts from its day and replaces the earlier from then.">
        {problem && <Refusal problem={problem} title="The rate was not saved" />}
        <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-who`}>
              Person
            </label>
            <select className="fy-select" id={`${id}-who`} value={who} onChange={(e) => setWho(e.target.value)} {...field("who")}>
              <option value="">Choose</option>
              <option value="standard">The standard rate (no person)</option>
              {(q.data?.people ?? []).map((p) => (
                <option key={p.user_id} value={p.user_id}>
                  {p.display_name}
                </option>
              ))}
            </select>
            {error("who")}
          </div>
          {who !== "standard" && (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-role`}>
                Role
              </label>
              <input className="fy-input" id={`${id}-role`} value={role} maxLength={80} onChange={(e) => setRole(e.target.value)} {...field("role")} />
              {error("role")}
            </div>
          )}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-from`}>
              Starts
            </label>
            <input className="fy-input" id={`${id}-from`} type="date" value={from} onChange={(e) => setFrom(e.target.value)} {...field("from")} />
            {error("from")}
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-rate`}>
              Cost of an hour
            </label>
            <input className="fy-input fy-num" id={`${id}-rate`} inputMode="decimal" value={rate} onChange={(e) => setRate(e.target.value)} {...field("rate")} />
            {error("rate")}
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
    </div>
  );
}
