import { useQuery } from "@tanstack/react-query";
import { CreditCard, Download, FileText, Printer } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { tokenOf } from "./DocumentView";

type Money = components["schemas"]["Money"];

const money = (m: Money) => `${m.currency} ${formatAmount(m.amount)}`;

function iso(d: Date): string {
  return d.toLocaleDateString("en-CA");
}

// Statement is the customer's statement of account through the portal's link: the currency and a period (the last 30 days by default,
// and never one that ends after today), as a table to read or print (FR-AR-14, FR-AR-08).
function Statement({ token, currencies, today }: { token: string; currencies: string[]; today: string }) {
  const id = useId();
  const [currency, setCurrency] = useState(currencies[0] ?? "");
  const [from, setFrom] = useState(() => iso(new Date(new Date(today).getTime() - 30 * 86400000)));
  const [to, setTo] = useState(today);
  const [shown, setShown] = useState<{ currency: string; from: string; to: string } | null>(null);
  const one = useQuery({
    queryKey: ["portal-statement", token, shown],
    enabled: Boolean(shown),
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/public/portal/statement", {
        params: { header: { "Fyniq-Portal": token }, query: { currency: shown?.currency || undefined, from: shown?.from, to: shown?.to } },
      });
      if (error) throw error;
      return data;
    },
  });
  const st = one.data;
  return (
    <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-head`}>
      <h2 className="fy-h3" id={`${id}-head`}>
        {helpText("public.portal-statement").label}
      </h2>
      <form
        className="fy-row fy-row--wrap gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          setShown({ currency, from, to });
        }}
      >
        {currencies.length > 1 && (
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-cur`}>
              Currency
            </label>
            <select className="fy-input" id={`${id}-cur`} value={currency} onChange={(e) => setCurrency(e.target.value)}>
              {currencies.map((c) => (
                <option key={c}>{c}</option>
              ))}
            </select>
          </div>
        )}
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-from`}>
            From
          </label>
          <input className="fy-input" id={`${id}-from`} type="date" value={from} max={to} onChange={(e) => setFrom(e.target.value)} />
        </div>
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-to`}>
            To
          </label>
          <input className="fy-input" id={`${id}-to`} type="date" value={to} max={today} onChange={(e) => setTo(e.target.value)} />
        </div>
        <div className="fy-field self-end">
          <Button type="submit" variant="secondary">
            Show the statement
          </Button>
        </div>
      </form>
      {one.isError && <Refusal problem={problemOf(one.error)} title="The statement could not be shown" />}
      {st && (
        <div className="fy-stack fy-stack--2">
          <table className="fy-table fy-table--compact" aria-label={`Statement of account, ${st.currency}`}>
            <thead>
              <tr>
                <th scope="col">Date</th>
                <th scope="col">What</th>
                <th scope="col" className="fy-num">
                  Amount
                </th>
                <th scope="col" className="fy-num">
                  Balance
                </th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td>{date(st.from)}</td>
                <td>Brought forward</td>
                <td />
                <td className="fy-num">{formatAmount(st.opening.amount)}</td>
              </tr>
              {st.lines.map((l, i) => (
                <tr key={i}>
                  <td>{date(l.date)}</td>
                  <td>{l.description}</td>
                  <td className="fy-num">{formatAmount(l.amount.amount)}</td>
                  <td className="fy-num">{formatAmount(l.balance.amount)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className="fy-strong">
            You owe {money(st.closing)} on {date(st.to)}
            {st.overdue.amount !== "0.00" && st.overdue.amount !== "0" ? `, of which ${money(st.overdue)} is past due` : ""}.
          </p>
          <div>
            <Button type="button" variant="ghost" size="sm" onClick={() => window.print()}>
              <Icon of={Printer} />
              Print
            </Button>
          </div>
        </div>
      )}
    </section>
  );
}

// CustomerPortal is what a customer sees through the portal link their supplier sent (docs/design/screens CustomerPortal; FR-AR-14): the
// invoices still due with a button to pay where the supplier made a payment link, each invoice's PDF, and a statement of account. No sign-in:
// the link's token, from the address's fragment, is the key, sent only in the Fyniq-Portal header; the page shows that customer's data and nothing else.
export function CustomerPortal() {
  const token = tokenOf(typeof window === "undefined" ? "" : window.location.hash);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState("");
  const one = useQuery({
    queryKey: ["portal", token],
    enabled: Boolean(token),
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/public/portal", { params: { header: { "Fyniq-Portal": token } } });
      if (error) throw error;
      return data;
    },
  });
  const p = one.data;
  const download = async (invoice: string, number: string) => {
    setBusy(invoice);
    setProblem(null);
    const { data, error } = await api.GET("/v1/public/portal/invoices/{invoice_id}/pdf", { params: { header: { "Fyniq-Portal": token }, path: { invoice_id: invoice } } });
    setBusy("");
    if (error || !data) return setProblem(problemOf(error));
    const bytes = Uint8Array.from(atob(data.pdf), (c) => c.charCodeAt(0));
    const url = URL.createObjectURL(new Blob([bytes], { type: "application/pdf" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `${number}.pdf`;
    a.click();
    URL.revokeObjectURL(url);
  };
  if (!token) {
    return (
      <main className="shell fy-stack" id="main">
        <h1 className="fy-h1">This link is incomplete</h1>
        <p className="fy-lead">Open the link as it was sent: the part after # is its key.</p>
      </main>
    );
  }
  return (
    <main className="shell fy-stack fy-stack--4" id="main">
      {one.isPending && <p className="fy-secondary">Opening your portal…</p>}
      {one.isError && (
        <>
          <h1 className="fy-h1">This link does not open anything</h1>
          <Refusal problem={problemOf(one.error)} title="The link is not valid, has ended or was withdrawn" />
          <p className="fy-secondary">Ask the company for a new link.</p>
        </>
      )}
      {p && (
        <>
          <header className="fy-stack fy-stack--2">
            {p.logo && <img src={`data:image/png;base64,${p.logo}`} alt={`${p.supplier} logo`} style={{ height: 40, width: "auto", maxWidth: 240 }} />}
            <p className="fy-secondary">
              {p.supplier} · for {p.customer}
            </p>
            <h1 className="fy-h1">{helpText("public.portal").label}</h1>
            <p className="fy-lead">{p.invoices.length === 0 ? "Nothing is due. Thank you." : `${p.invoices.length} invoice${p.invoices.length === 1 ? "" : "s"} due`}</p>
          </header>
          {problem && <Refusal problem={problem} title="The PDF could not be downloaded" />}
          {p.invoices.length > 0 && (
            <section className="fy-card" aria-label="Invoices due">
              <div className="fy-card__body">
                <table className="fy-table" aria-label="Invoices due">
                  <thead>
                    <tr>
                      <th scope="col">Invoice</th>
                      <th scope="col">Due</th>
                      <th scope="col" className="fy-num">
                        Total
                      </th>
                      <th scope="col" className="fy-num">
                        Still due
                      </th>
                      <th scope="col">
                        <span className="sr-only">Actions</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {p.invoices.map((i) => (
                      <tr key={i.id}>
                        <th scope="row">
                          <span className="fy-strong">{i.number}</span>
                          <span className="fy-small fy-muted"> dated {date(i.date)}</span>
                        </th>
                        <td>
                          {i.due_date ? date(i.due_date) : ""}
                          {i.overdue && <span className="fy-chip fy-chip--critical"> Overdue</span>}
                        </td>
                        <td className="fy-num">{money(i.total)}</td>
                        <td className="fy-num fy-strong">{money(i.open)}</td>
                        <td>
                          <div className="fy-row fy-row--wrap gap-2">
                            {i.payment_url && (
                              <a className="fy-button fy-button--sm fy-button--primary" href={i.payment_url} rel="noopener noreferrer" title={helpText("public.portal-pay").tooltip}>
                                <Icon of={CreditCard} />
                                {helpText("public.portal-pay").label}
                              </a>
                            )}
                            <Button type="button" size="sm" variant="secondary" disabled={busy === i.id} onClick={() => void download(i.id, i.number)} title={helpText("public.download").tooltip}>
                              <Icon of={Download} />
                              PDF
                            </Button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                {p.invoices.some((i) => !i.payment_url) && (
                  <p className="fy-small fy-muted">Where there is no Pay button, pay as the invoice says; {p.supplier} records it when it arrives.</p>
                )}
              </div>
            </section>
          )}
          {p.statement_currencies.length > 0 && <Statement token={token} currencies={p.statement_currencies.map((c) => c.currency)} today={p.today} />}
          <p className="fy-small fy-muted">
            <Icon of={FileText} /> This link works until {date(p.link_expires_at.slice(0, 10))} at the latest. Keep it private: anyone who holds it sees what you see.
          </p>
        </>
      )}
    </main>
  );
}
