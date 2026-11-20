import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, Hourglass, Link2Off, TriangleAlert } from "lucide-react";
import { api } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { date } from "../../format/date";
import { formatAmount } from "../../format/amount";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";
import { SalaryState } from "./SalaryPayments";

const features: Record<string, string> = { records: "People records", payroll: "Payroll", leave: "Leave", letters: "Letters and self-service" };
const where: Record<string, string> = { nemura: "Nemura", external: "An external system", none: "Manual journal" };

// Payroll is Payroll in the books (docs/design/screens Payroll; FR-PPL-04, FR-PPL-05, FR-PPL-08, FR-PAY-07): Fyniq's part of payroll in one place: where each feature lives, the state of the link, how
// the month's statutory outputs are met and the salary batches that came with the pay runs. Each part is its own read, so a role that may not see one still sees the others (the API decides).
export function Payroll() {
  useSignedIn();
  const month = new Date().toISOString().slice(0, 7);
  const link = useQuery({
    queryKey: ["people-link"],
    queryFn: async () => {
      const r = await api.GET("/v1/people-link", {});
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const health = useQuery({
    queryKey: ["people-link-health"],
    enabled: link.data?.state === "active",
    queryFn: async () => {
      const r = await api.GET("/v1/people-link/health", {});
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
  const batches = useQuery({
    queryKey: ["salary-batches"],
    queryFn: async () => {
      const r = await api.GET("/v1/salary-batches", {});
      if (r.error) throw r.error;
      return r.data.items;
    },
  });
  const people = useQuery({
    queryKey: ["people-projection-list"],
    enabled: link.data?.state === "active",
    queryFn: async () => {
      const r = await api.GET("/v1/people-link/people", { params: { query: { page_size: 20 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const away = useQuery({
    queryKey: ["people-availability"],
    enabled: link.data?.state === "active",
    queryFn: async () => {
      const r = await api.GET("/v1/people-link/availability", { params: { query: { page_size: 20 } } });
      if (r.error) throw r.error;
      return r.data.items ?? [];
    },
  });
  const state = link.data?.state ?? "none";
  const nearly = health.data && health.data.reachable && (health.data.failing > 0 || health.data.refused > 0 || health.data.missing.length > 0);
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Payroll</span>
          </nav>
          <h1 className="fy-h1">Payroll in the books</h1>
          <p className="fy-secondary">Fyniq's part of payroll: the pay runs that arrive from Nemura and what they post, how the statutory outputs are met, and the salary payments.</p>
        </div>
      </div>
      <ReviewBanner screen="payroll-overview" />
      <section aria-label="Nemura link" className="fy-stack fy-stack--2">
        <h2 className="fy-h2">Nemura link</h2>
        {link.isError && <Refusal problem={problemOf(link.error)} title="The link could not be read" />}
        {link.data && (
          <>
            <p>
              <span className={`fy-chip fy-chip--${state === "active" ? "good" : state === "none" || state === "unlinked" ? "neutral" : "warning"}`}>
                <Icon of={state === "active" ? CircleCheck : state === "none" || state === "unlinked" ? Link2Off : Hourglass} />
                {state === "none" ? "Not linked" : state.charAt(0).toUpperCase() + state.slice(1)}
              </span>{" "}
              {state === "none" && "Payroll is posted by manual journal or from a payroll provider's file."}
            </p>
            {nearly && (
              <div className="fy-banner fy-banner--warning" role="note">
                <Icon of={TriangleAlert} size={20} />
                <div className="fy-banner__text">
                  <span className="fy-strong">The link is not delivering everything.</span>
                  <span className="fy-small">
                    {health.data?.failing} failing, {health.data?.refused} refused, {health.data?.missing.length} gap(s) in what Nemura says it sent. See Settings, People and HR.
                  </span>
                </div>
              </div>
            )}
            <section aria-label="What lives in Nemura">
              <h3 className="fy-h3">What lives in Nemura</h3>
              <ul className="fy-stack fy-stack--1">
                {Object.entries(features).map(([k, label]) => (
                  <li key={k}>
                    {label}: {where[link.data?.sources?.find((s) => s.feature === k)?.source ?? "none"] ?? "Manual journal"}
                  </li>
                ))}
              </ul>
            </section>
            <p>
              <Link to="/settings/people">People and HR settings</Link>
            </p>
          </>
        )}
      </section>
      {people.data && people.data.length > 0 && (
        <section aria-label="People from Nemura" className="fy-stack fy-stack--2">
          <h2 className="fy-h2">People from Nemura</h2>
          <ul className="fy-stack fy-stack--1">
            {people.data.map((p) => (
              <li key={p.person_id}>
                <Link to="/payroll/employees/$personId" params={{ personId: p.person_id }}>
                  {p.display_name ?? p.person_id}
                </Link>
                {p.held ? " (held for tax clearance)" : ""}
                {p.removed ? " (removed)" : ""}
              </li>
            ))}
          </ul>
        </section>
      )}
      {away.data && away.data.length > 0 && (
        <section aria-label="Away in the next 30 days" className="fy-stack fy-stack--2">
          <h2 className="fy-h2">Away in the next 30 days</h2>
          <ul className="fy-stack fy-stack--1">
            {away.data.map((x) => (
              <li key={x.entry_id}>
                {x.person_name ?? x.person_id}: {date(x.start_date)}
                {x.start_date === x.end_date ? "" : ` to ${date(x.end_date)}`}, {x.days} day(s)
              </li>
            ))}
          </ul>
          <p className="fy-secondary">Nemura tells Fyniq when approved leave starts and ends, and nothing of its kind or reason. It moves no money.</p>
        </section>
      )}
      <p>
        <Link to="/payroll/runs/$month" params={{ month }}>
          Pay runs received for {month}
        </Link>
      </p>
      <section aria-label="Statutory outputs" className="fy-stack fy-stack--2">
        <h2 className="fy-h2">How {month} is met</h2>
        {gate.isError && <p className="fy-secondary">The month's arrangements are not shown to your role.</p>}
        {gate.data &&
          (gate.data.met ? (
            <p>
              <Icon of={CircleCheck} /> Every statutory output of the month has a recorded arrangement.
            </p>
          ) : (
            <div className="fy-banner fy-banner--warning" role="note">
              <Icon of={TriangleAlert} size={20} />
              <div className="fy-banner__text">
                <span className="fy-strong">{gate.data.missing.length} arrangement(s) are not recorded: a pay run is not approved for payment until they are.</span>
                <span className="fy-small">Record each in Settings, People and HR, with its evidence.</span>
              </div>
            </div>
          ))}
      </section>
      <section aria-label="Salary batches" className="fy-stack fy-stack--2">
        <h2 className="fy-h2">Salary payments</h2>
        {batches.isError && <Refusal problem={problemOf(batches.error)} title="The salary payments could not be shown" />}
        {batches.data && batches.data.length === 0 && <p className="fy-secondary">No pay run has sent its salary lines yet. They arrive when a pay run is approved in Nemura and posted here.</p>}
        {batches.data && batches.data.length > 0 && (
          <table className="fy-table" aria-label="Latest salary batches">
            <thead>
              <tr>
                <th scope="col">Batch</th>
                <th scope="col">Wage month</th>
                <th scope="col">State</th>
                <th scope="col" className="fy-num">
                  Total (SGD)
                </th>
              </tr>
            </thead>
            <tbody>
              {batches.data.slice(0, 5).map((b) => (
                <tr key={b.id}>
                  <th scope="row">
                    <Link to="/payroll/salary-payments/$batchId" params={{ batchId: b.id }}>
                      {b.number}
                    </Link>
                  </th>
                  <td>{date(`${b.wage_month}-01`).replace(/^\d+ /, "")}</td>
                  <td>
                    <SalaryState batch={b} />
                  </td>
                  <td className="fy-num">{formatAmount(b.total.amount)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <p>
          <Link to="/payroll/salary-payments">All salary payments</Link>
        </p>
      </section>
    </div>
  );
}
