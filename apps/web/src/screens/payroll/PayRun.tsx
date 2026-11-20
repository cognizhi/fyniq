import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { CircleCheck, CircleX, FileCheck2, Hourglass, Minus } from "lucide-react";
import { api } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { formatAmount } from "../../format/amount";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";

// PayRun is one wage month's pay runs as Fyniq received them from Nemura (docs/design/screens PayRun; FR-PPL-09, FR-PPL-10, FR-PPL-11, FR-PAY-13): each version's steps (received and verified, then posted or unchanged),
// what it has posted by account and never by person, and the runs Fyniq refused with the code and its plain words. Preparing, approving and releasing stay in Nemura and in the salary batch.
export function PayRun() {
  useSignedIn();
  const { month } = useParams({ strict: false }) as { month: string };
  const h = helpText("payroll.pay-run");
  const runs = useQuery({
    queryKey: ["payroll-runs", month],
    queryFn: async () => {
      const r = await api.GET("/v1/payroll-runs", { params: { query: { month } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const accounts = useQuery({
    queryKey: ["accounts", "pay-run"],
    queryFn: async () => {
      const r = await api.GET("/v1/accounts", { params: { query: { page_size: 500 } } });
      if (r.error) throw r.error;
      return new Map((r.data.items ?? []).map((a) => [a.id, `${a.code} ${a.name}`]));
    },
  });
  const [y, m] = month.split("-").map(Number);
  const prev = m === 1 ? `${y! - 1}-12` : `${y}-${String(m! - 1).padStart(2, "0")}`;
  const next = m === 12 ? `${y! + 1}-01` : `${y}-${String(m! + 1).padStart(2, "0")}`;
  const items = runs.data?.items ?? [];
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/payroll">Payroll</Link>
          </nav>
          <h1 className="fy-h1" title={h.tooltip}>
            {h.label}: {date(`${month}-01`).replace(/^\d+ /, "")}
          </h1>
          <p className="fy-secondary">What arrived from Nemura for this wage month, what each version posted, and what Fyniq refused. Names and salaries stay in Nemura: Fyniq sees totals by account and department.</p>
        </div>
      </div>
      <ReviewBanner screen="pay-run" />
      <p className="fy-row gap-2">
        <Link to="/payroll/runs/$month" params={{ month: prev }}>
          Earlier month
        </Link>
        <Link to="/payroll/runs/$month" params={{ month: next }}>
          Later month
        </Link>
      </p>
      {runs.isError && <Refusal problem={problemOf(runs.error)} title="The pay runs could not be shown" />}
      {runs.data && items.length === 0 && <p className="fy-secondary">No pay run has arrived for this month. A run is approved in Nemura, with its approver's passkey, and then posted here.</p>}
      {items.map((x) => (
        <section key={`${x.run_id}-${x.version}`} aria-label={`Version ${x.version}`} className="fy-card fy-stack fy-stack--2">
          <h2 className="fy-h2">
            Run {x.run_id.slice(0, 8)}, version {x.version}
          </h2>
          <ol className="fy-stack fy-stack--1" aria-label="Steps">
            <li>
              <Icon of={CircleCheck} /> Received {dateTime(x.received_at)} and verified: Nemura's signature, the approver's passkey and the account mapping (version {x.mapping_version}).
            </li>
            <li>
              {x.outcome === "posted" ? (
                <>
                  <Icon of={FileCheck2} /> Posted as a journal entry.
                </>
              ) : (
                <>
                  <Icon of={Minus} /> Changed nothing: only the version was recorded.
                </>
              )}
            </li>
          </ol>
          {x.posted.length > 0 && (
            <table className="fy-table" aria-label={`Posted by account, version ${x.version}`}>
              <thead>
                <tr>
                  <th scope="col">Account</th>
                  <th scope="col" className="fy-num">
                    Total so far
                  </th>
                </tr>
              </thead>
              <tbody>
                {x.posted.map((l) => (
                  <tr key={l.account_id}>
                    <th scope="row">{accounts.data?.get(l.account_id) ?? l.account_id}</th>
                    <td className="fy-num">{formatAmount(l.amount)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>
      ))}
      {runs.data && runs.data.refused.length > 0 && (
        <section aria-label="Refused" className="fy-stack fy-stack--2">
          <h2 className="fy-h2">Refused and not posted since</h2>
          {runs.data.refused.map((r) => (
            <div key={`${r.run_id}-${r.version}`} className="fy-banner fy-banner--critical" role="note">
              <Icon of={CircleX} size={20} />
              <div className="fy-banner__text">
                <span className="fy-strong">
                  Run {r.run_id.slice(0, 8)}, version {r.version}: {r.message}
                </span>
                <span className="fy-small">
                  {r.code}, {dateTime(r.refused_at)}. Put right what it says, then send the run again from Nemura or approve a new version.
                </span>
              </div>
            </div>
          ))}
        </section>
      )}
      {runs.isLoading && (
        <p className="fy-secondary">
          <Icon of={Hourglass} /> Loading the pay runs…
        </p>
      )}
    </div>
  );
}
