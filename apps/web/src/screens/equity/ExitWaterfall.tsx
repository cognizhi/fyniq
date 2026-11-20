import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { FlaskConical } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { formatAmount } from "../../format/amount";
import { helpText } from "../../i18n";
import { Icon, problemOf } from "../../ui";

type Waterfall = components["schemas"]["ExitWaterfall"];
type ClassView = components["schemas"]["WaterfallClassView"];
type Money = components["schemas"]["Money"];

const money = (m: Money | undefined) => (m ? formatAmount(m.amount) : "—");

// ExitWaterfall is Equity › Exit waterfall (docs/design/screens ExitWaterfall; FR-EQ-29): a sandbox that shows what an exit value would pay each class and holder under the liquidation
// preferences the register records, next to every class converted with no preference. It reads the register and records nothing. A structure it does not model is refused by name.
export function ExitWaterfall() {
  const id = useId();
  const [value, setValue] = useState("8000000.00");
  const valid = /^[0-9]+(\.[0-9]+)?$/.test(value);
  const q = useQuery({
    queryKey: ["exit-waterfall", value],
    enabled: valid,
    retry: false,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/exit-waterfall", { params: { query: { exit_value: value } } });
      if (error) throw error;
      return data as Waterfall;
    },
  });
  const w = q.data;
  const h = helpText("equity.exit-waterfall");
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/equity">Equity</Link>
          </nav>
          <span className="fy-eyebrow">Equity · sandbox</span>
          <h1 className="fy-h1">{h.label}</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
      </div>
      <div className="fy-banner fy-banner--info">
        <Icon of={FlaskConical} size={20} />
        <div className="fy-banner__text">
          <span className="fy-strong">A sandbox on the recorded shares</span>
          <span className="fy-small">It reads the register and the classes' recorded preferences. Nothing here is posted or saved.</span>
        </div>
      </div>
      <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-label="Exit">
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-value`}>
            Exit value ({w?.currency ?? "SGD"})
          </label>
          <input id={`${id}-value`} className="fy-input fy-num" inputMode="decimal" value={value} onChange={(e) => setValue(e.target.value)} aria-describedby={`${id}-help`} />
          <span className="fy-help" id={`${id}-help`}>
            What the shareholders receive after debts and sale costs, as a plain number such as 8000000.00.
          </span>
        </div>
        {!valid && <p className="fy-error">Enter the exit value as a plain number, such as 8000000.00.</p>}
      </section>
      {q.isError && <Refusal problem={problemOf(q.error)} title="The waterfall could not be modelled" />}
      {w && !w.available && (
        <div className="fy-empty fy-card fy-card--pad" role="status">
          <h2 className="fy-h3">No waterfall to show</h2>
          <p className="fy-secondary">{w.message}</p>
        </div>
      )}
      {w?.available && (
        <>
          <Table title="Under the preferences" classes={w.classes} currency={w.currency} />
          <Table title="As converted: every share the same" classes={w.as_converted} currency={w.currency} />
          {w.notes.length > 0 && (
            <ul className="fy-list fy-small" aria-label="What the waterfall leaves out">
              {w.notes.map((n) => (
                <li key={n} className="fy-list__item">
                  {n}
                </li>
              ))}
            </ul>
          )}
          <p className="fy-small fy-secondary">Modelled: {w.modelled.join("; ")}. A capped participation or a cumulative dividend is refused, never approximated [lawyer to confirm].</p>
        </>
      )}
    </div>
  );
}

function Table({ title, classes, currency }: { title: string; classes: ClassView[]; currency: string }) {
  return (
    <section className="fy-stack fy-stack--2">
      <h2 className="fy-h3">{title}</h2>
      <div className="fy-tablewrap">
        <table className="fy-table fy-table--compact" aria-label={title}>
          <thead>
            <tr>
              <th scope="col">Class and holder</th>
              <th scope="col" className="fy-num">
                Shares
              </th>
              <th scope="col" className="fy-num">
                Invested
              </th>
              <th scope="col" className="fy-num">
                Preference
              </th>
              <th scope="col" className="fy-num">
                Receives ({currency})
              </th>
            </tr>
          </thead>
          {classes.map((c) => (
            <tbody key={c.class_id}>
              <tr>
                <th scope="row" className="fy-strong">
                  {c.name}
                  {c.has_preference ? ` · ${c.multiple}x${c.participating ? ", participating" : ""}${c.converts ? " · converts" : ""}` : ""}
                </th>
                <td />
                <td />
                <td className="fy-num">{money(c.preference)}</td>
                <td className="fy-num fy-strong">{money(c.total)}</td>
              </tr>
              {c.holders.map((h) => (
                <tr key={h.holder_id}>
                  <th scope="row" className="fy-secondary">
                    {h.holder_name}
                  </th>
                  <td className="fy-num">{formatAmount(h.shares)}</td>
                  <td className="fy-num">{money(h.invested)}</td>
                  <td />
                  <td className="fy-num">{money(h.total)}</td>
                </tr>
              ))}
            </tbody>
          ))}
        </table>
      </div>
    </section>
  );
}
