import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, PencilLine, UserPlus } from "lucide-react";
import { useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { helpText } from "../../i18n";
import { Icon, problemOf } from "../../ui";

type Holder = components["schemas"]["Holder"];

// EquityTabs are the equity register's views (docs/design/screens Holders): the cap table, the holders, the share
// classes, the register of share issues, and the SAFEs and notes. The option pool joins them as it is built.
export function EquityTabs({ current }: { current: "cap-table" | "holders" | "classes" | "transactions" | "instruments" | "options" | "rounds" | "reconciliation" }) {
  const tabs = [
    { key: "cap-table", to: "/equity", label: "Cap table" },
    { key: "holders", to: "/equity/holders", label: "Holders" },
    { key: "classes", to: "/equity/classes", label: "Classes" },
    { key: "transactions", to: "/equity/transactions", label: "Transactions" },
    { key: "instruments", to: "/equity/instruments", label: "SAFEs and notes" },
    { key: "options", to: "/equity/options", label: "Option pool" },
    { key: "rounds", to: "/equity/rounds", label: "Round simulator" },
    { key: "reconciliation", to: "/equity/reconciliation", label: "Reconciliation" },
  ] as const;
  return (
    <nav className="fy-tabs" aria-label="Equity views">
      {tabs.map((t) => (
        <Link key={t.key} to={t.to} aria-current={t.key === current ? "page" : undefined}>
          {t.label}
        </Link>
      ))}
    </nav>
  );
}

export async function listHolders(): Promise<Holder[]> {
  const { data, error } = await api.GET("/v1/holders", { params: { query: { page_size: 200 } } });
  if (error) throw error;
  return data.items ?? [];
}

// holdsText says what a holder holds, class by class, in shares.
export function holdsText(h: Holder): string {
  if (h.holdings.length === 0) return h.investor ? "No shares yet" : "No shares recorded";
  return h.holdings.map((x) => `${Number(x.shares).toLocaleString("en-SG")} ${x.class_name.toLowerCase()}`).join(", ");
}

// kindText describes a holder: individual or entity, and what else they are to the company.
export function kindText(h: Holder): string {
  const roles = [h.director ? "director" : "", h.shareholder ? "shareholder" : "", h.investor ? "investor" : ""].filter(Boolean).join(", ");
  const kind = h.kind === "entity" ? "Entity" : h.kind === "individual" ? "Individual" : "No record yet";
  return `${kind} · ${roles}`;
}

type Filter = "all" | "shareholders" | "investors";

// Holders is Equity › Holders (docs/design/screens Holders; FR-EQ-15, FR-EQ-27 Expert): the shareholders and investors,
// each a contact, with the identifier from their record in force (an individual's last 4 characters only) and their
// shares by class. Holders are added by marking a contact shareholder or investor.
export function Holders() {
  const [filter, setFilter] = useState<Filter>("all");
  const [search, setSearch] = useState("");
  const q = useQuery({ queryKey: ["holders"], queryFn: listHolders });
  const add = helpText("equity.add-holder");
  const all = q.data ?? [];
  const shown = all
    .filter((h) => filter === "all" || (filter === "shareholders" ? h.shareholder : h.investor))
    .filter((h) => !search.trim() || h.name.toLowerCase().includes(search.trim().toLowerCase()));
  const count = (f: Filter) => all.filter((h) => f === "all" || (f === "shareholders" ? h.shareholder : h.investor)).length;
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Equity</span>
          <h1 className="fy-h1" title={helpText("equity.holders").tooltip}>
            Shareholders and investors
          </h1>
          <p className="fy-secondary">Each holder is a contact marked shareholder or investor. Their details are recorded on another person's approval.</p>
        </div>
        <div className="fy-pagehead__actions">
          <Link className="fy-btn fy-btn--secondary" to="/contacts" title={add.tooltip}>
            <Icon of={UserPlus} />
            {add.label}
          </Link>
        </div>
      </div>
      <EquityTabs current="holders" />
      {q.isError && <Refusal problem={problemOf(q.error)} title="The holders could not be shown" />}
      {q.data && (
        <div className="fy-stack fy-stack--3">
          <div className="fy-row fy-row--wrap fy-row--between gap-3">
            <div className="fy-chipset" role="group" aria-label="Show">
              {(["all", "shareholders", "investors"] as const).map((f) => (
                <button key={f} className="fy-filter" type="button" aria-pressed={filter === f} onClick={() => setFilter(f)}>
                  {{ all: "All", shareholders: "Shareholders", investors: "Investors" }[f]} {count(f)}
                </button>
              ))}
            </div>
            <div className="fy-field" style={{ width: "100%", maxWidth: 260 }}>
              <label className="fy-sr" htmlFor="holders-search">
                Search holders
              </label>
              <input className="fy-input" id="holders-search" placeholder="Search by name" value={search} onChange={(e) => setSearch(e.target.value)} />
            </div>
          </div>
          {all.length === 0 && (
            <p className="fy-small fy-muted">
              No holders yet. Mark a contact as a shareholder or an investor in Contacts, then record their details here.
            </p>
          )}
          {all.length > 0 && (
            <div className="fy-card">
              <table className="fy-table fy-table--cards" aria-label="Holders">
                <thead>
                  <tr>
                    <th scope="col">Holder</th>
                    <th scope="col" className="fy-from-expanded">
                      Identifier
                    </th>
                    <th scope="col">Holds</th>
                    <th scope="col">Record</th>
                  </tr>
                </thead>
                <tbody className="fy-stagger">
                  {shown.map((h) => (
                    <tr key={h.contact_id}>
                      <td data-card="title">
                        <Link to="/equity/holders/$contactId" params={{ contactId: h.contact_id }}>
                          {h.name}
                        </Link>
                        <div className="fy-small fy-muted">{kindText(h)}</div>
                      </td>
                      <td data-card="hide" className="fy-from-expanded">
                        {h.identifier ? <span className="fy-mono">{h.identifier}</span> : <span className="fy-muted">–</span>}
                      </td>
                      <td data-card="meta">{holdsText(h)}</td>
                      <td data-card="status">
                        {h.recorded ? (
                          <span className="fy-chip fy-chip--good">
                            <Icon of={CircleCheck} />
                            Recorded
                          </span>
                        ) : (
                          <span className="fy-chip fy-chip--neutral">
                            <Icon of={PencilLine} />
                            {h.pending_record_id ? "Draft" : "No record"}
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
