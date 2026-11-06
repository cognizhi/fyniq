import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRight, CircleCheck, FileSearch, TriangleAlert } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf } from "../../ui";

type Found = components["schemas"]["EvidenceFound"];
type Activity = NonNullable<Found["record"]["activity"]>;

// The kinds of record, as a filter names them.
const activities: Record<Activity, string> = {
  bill: "Bills and vendor credits",
  invoice: "Invoices and credit notes",
  journal: "Journals",
  bank: "Bank transactions and agreements",
  claim: "Expense claims",
  vendor_bank_change: "Changes of suppliers' bank details",
  person_bank_change: "Changes of people's bank accounts",
  founders: "Founders' money",
  owner_succession: "Owner successions",
};

// recordLink is where a record's page is, for the kinds that have one.
function recordLink(r: Found["record"]): string | undefined {
  switch (r.type) {
    case "bill":
    case "vendor_credit":
      return `/purchases/bills/${r.id}`;
    case "invoice":
    case "credit_note":
      return `/sales/invoices/${r.id}`;
    case "journal":
      return `/accounting/journals/${r.id}`;
    default:
      return undefined;
  }
}

interface Filters {
  activity: string;
  evidenceType: string;
  from: string;
  to: string;
}

// EvidenceSearch is Compliance › Evidence (docs/design/screens EvidenceSearch; FR-DOC-10, PRD-001 rev 0.184,
// Expert): every file across the records the person views, found by activity, evidence type and the record's date,
// and the records still missing their evidence (C.2 #30).
export function EvidenceSearch() {
  const id = useId();
  const [filters, setFilters] = useState<Filters>({ activity: "", evidenceType: "", from: "", to: "" });
  const [applied, setApplied] = useState<Filters>(filters);
  const found = useQuery({
    queryKey: ["evidence-search", applied],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/evidence", {
        params: {
          query: {
            activity: (applied.activity || undefined) as Activity | undefined,
            evidence_type: applied.evidenceType.trim() || undefined,
            from: applied.from || undefined,
            to: applied.to || undefined,
            page_size: 100,
          },
        },
      });
      if (error) throw error;
      return data;
    },
  });
  const missing = useQuery({
    queryKey: ["check", "evidence.missing"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/checks/{key}", { params: { path: { key: "evidence.missing" } } });
      if (error) throw error;
      return data;
    },
  });
  const h = helpText("compliance.evidence");
  const may = found.data?.activities ?? [];
  const items = found.data?.items ?? [];
  const missed = missing.data?.items ?? [];

  return (
    <div className="fy-page fy-enter">
      <nav className="fy-breadcrumb" aria-label="Breadcrumb">
        <Link to="/accounting">Accounting</Link>
        <Icon of={ChevronRight} />
        <span>Evidence</span>
      </nav>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">{h.label}</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
      </div>

      <section className="fy-card" aria-labelledby={`${id}-missing`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-missing`}>
            Missing evidence
          </h2>
          <span className="fy-badge fy-badge--quiet">{missed.length}</span>
        </div>
        <div className="fy-card__body">
          {missing.isError && <p className="fy-small fy-secondary">Your role does not read this check.</p>}
          {missing.data && missed.length === 0 && (
            <p className="fy-small fy-row gap-2">
              <Icon of={CircleCheck} />
              Every posted record of the open months has its evidence.
            </p>
          )}
          {missed.length > 0 && (
            <ul className="fy-list" aria-labelledby={`${id}-missing`}>
              {missed.map((m) => (
                <li key={m.id} className="fy-list__item">
                  <span className="fy-chip fy-chip--warning">
                    <Icon of={TriangleAlert} />
                    {m.kind === "journal" ? "Journal" : "Bill"}
                  </span>
                  <span className="fy-list__main">
                    <span className="fy-list__title whitespace-normal">
                      {m.kind === "journal" ? <Link to="/accounting/journals/$journalId" params={{ journalId: m.id ?? "" }}>{m.label}</Link> : <Link to="/purchases/bills/$billId" params={{ billId: m.id ?? "" }}>{m.label}</Link>}
                    </span>
                    <span className="fy-list__meta">{m.detail}</span>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </section>

      <section className="fy-card" aria-labelledby={`${id}-search`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-search`}>
            Find evidence
          </h2>
        </div>
        <form
          className="fy-card__body fy-row fy-row--wrap gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            setApplied(filters);
          }}
        >
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-activity`}>
              Records
            </label>
            <select className="fy-select" id={`${id}-activity`} value={filters.activity} onChange={(e) => setFilters({ ...filters, activity: e.target.value })}>
              <option value="">All you may see</option>
              {may.map((a) => (
                <option key={a} value={a}>
                  {activities[a as Activity] ?? a}
                </option>
              ))}
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-type`}>
              Evidence type
            </label>
            <input className="fy-input" id={`${id}-type`} placeholder="invoice" value={filters.evidenceType} onChange={(e) => setFilters({ ...filters, evidenceType: e.target.value })} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-from`}>
              Records from
            </label>
            <input className="fy-input" id={`${id}-from`} type="date" value={filters.from} onChange={(e) => setFilters({ ...filters, from: e.target.value })} />
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-to`}>
              to
            </label>
            <input className="fy-input" id={`${id}-to`} type="date" value={filters.to} onChange={(e) => setFilters({ ...filters, to: e.target.value })} />
          </div>
          <button type="submit" className="fy-btn fy-btn--secondary self-end">
            <Icon of={FileSearch} />
            Find
          </button>
        </form>
        {found.isError && <Refusal problem={problemOf(found.error)} title="The evidence could not be searched" />}
        {found.data && items.length === 0 && <p className="fy-card__body fy-small fy-secondary">No file matches.</p>}
        {items.length > 0 && (
          <div className="overflow-x-auto">
            <table className="fy-table" aria-label="Evidence found">
              <thead>
                <tr>
                  <th scope="col">Record</th>
                  <th scope="col">Date</th>
                  <th scope="col">File</th>
                  <th scope="col">Evidence type</th>
                  <th scope="col">Attached</th>
                </tr>
              </thead>
              <tbody>
                {items.map((f) => {
                  const to = recordLink(f.record);
                  const name = f.record.number || activities[f.record.activity as Activity] || f.record.type;
                  return (
                    <tr key={f.id}>
                      <td>{to ? <a href={to}>{name}</a> : name}</td>
                      <td>{f.record.date ? date(f.record.date) : ""}</td>
                      <td className="fy-mono">
                        {f.file_name}
                        {f.unmasked_nric && (
                          <span className="fy-chip fy-chip--warning ml-2" title="Its text shows a Singapore NRIC or FIN in full (FR-DOC-09).">
                            <Icon of={TriangleAlert} />
                            Shows an NRIC
                          </span>
                        )}
                      </td>
                      <td>{f.evidence_type}</td>
                      <td>{dateTime(f.uploaded_at)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}
