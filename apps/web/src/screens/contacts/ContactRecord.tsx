import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { ChevronRight, History } from "lucide-react";
import { useId } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf } from "../../ui";

type Money = components["schemas"]["Money"];
type Row = components["schemas"]["ReceivablesRow"];
type Event = components["schemas"]["ContactEvent"];

const money = (m: Money) => `${formatAmount(m.amount)} ${m.currency}`;

// The documents' names, in the timeline's words.
const kinds: Record<string, string> = {
  invoice: "Invoice",
  credit_note: "Credit note",
  customer_receipt: "Receipt",
  customer_refund: "Refund",
  customer_deposit: "Deposit",
  customer_credit_application: "Credit applied",
  write_off: "Write-off",
  quote: "Quote",
  sales_order: "Sales order",
  bill: "Bill",
  vendor_credit: "Vendor credit",
  bill_payment: "Payment",
  supplier_refund: "Refund from supplier",
  credit_application: "Credit applied",
  purchase_order: "Purchase order",
};

function linkOf(e: Event): { to: string; params: Record<string, string> } | undefined {
  switch (e.type) {
    case "invoice":
    case "credit_note":
      return { to: "/sales/invoices/$invoiceId", params: { invoiceId: e.id } };
    case "bill":
    case "vendor_credit":
      return { to: "/purchases/bills/$billId", params: { billId: e.id } };
    default:
      return undefined;
  }
}

// totals are a side's totals by currency, as one line.
const totals = (rows: { total: Money }[]) => (rows.length === 0 ? "Nothing" : rows.map((r) => money(r.total)).join(" · "));

function Tile({ label, value, meta }: { label: string; value: string; meta: string }) {
  return (
    <div className="fy-tile">
      <span className="fy-tile__label">{label}</span>
      <span className="fy-tile__value fy-num">{value}</span>
      <span className="fy-tile__meta">{meta}</span>
    </div>
  );
}

function Ageing({ rows, label }: { rows: Row[]; label: string }) {
  if (rows.length === 0) return null;
  return (
    <div className="overflow-x-auto">
      <table className="fy-table" aria-label={label}>
        <thead>
          <tr>
            <th scope="col">Currency</th>
            <th scope="col" className="fy-num">
              Current
            </th>
            <th scope="col" className="fy-num">
              1–30
            </th>
            <th scope="col" className="fy-num">
              31–60
            </th>
            <th scope="col" className="fy-num">
              61–90
            </th>
            <th scope="col" className="fy-num">
              90+
            </th>
            <th scope="col" className="fy-num">
              Credits
            </th>
            <th scope="col" className="fy-num">
              Total
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.currency}>
              <td>{r.currency}</td>
              <td className="fy-num">{formatAmount(r.current.amount)}</td>
              <td className="fy-num">{formatAmount(r.days_1_30.amount)}</td>
              <td className="fy-num">{formatAmount(r.days_31_60.amount)}</td>
              <td className="fy-num">{formatAmount(r.days_61_90.amount)}</td>
              <td className="fy-num">{formatAmount(r.days_90_plus.amount)}</td>
              <td className="fy-num">{formatAmount(r.credits.amount)}</td>
              <td className="fy-num fy-strong">{formatAmount(r.total.amount)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// ContactRecord is Contacts › one contact (docs/design/screens ContactRecord; FR-CT-01, FR-CT-02, PRD-001 rev 0.185):
// what they owe and are owed, what was invoiced and billed in the financial year, the ageing, and their latest
// documents.
export function ContactRecord() {
  const { contactId } = useParams({ strict: false }) as { contactId: string };
  const id = useId();
  const contact = useQuery({
    queryKey: ["contact", contactId],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/contacts/{contact_id}", { params: { path: { contact_id: contactId } } });
      if (error) throw error;
      return data;
    },
  });
  const summary = useQuery({
    queryKey: ["contact-summary", contactId],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/contacts/{contact_id}/summary", { params: { path: { contact_id: contactId } } });
      if (error) throw error;
      return data;
    },
  });
  const h = helpText("contacts.record");
  const c = contact.data;
  const s = summary.data;
  if (contact.isError) return <Refusal problem={problemOf(contact.error)} title="The contact could not be read" />;
  return (
    <div className="fy-page fy-enter">
      <nav className="fy-breadcrumb" aria-label="Breadcrumb">
        <Link to="/contacts">Contacts</Link>
        <Icon of={ChevronRight} />
        <span>{c?.legal_name ?? "A contact"}</span>
      </nav>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">{c?.legal_name ?? "Contact"}</h1>
          <p className="fy-secondary">
            {[c?.customer ? "Customer" : "", c?.vendor ? "Vendor" : "", c?.uen_or_tax_id ?? "", c?.billing_email ?? ""].filter(Boolean).join(" · ")}
          </p>
        </div>
      </div>
      {summary.isError && <Refusal problem={problemOf(summary.error)} title="The balances could not be read" />}
      {s && (
        <>
          <div className="fy-grid fy-grid--4 gap-3" role="group" aria-label={h.label}>
            {c?.customer !== false && <Tile label="Owed to us" value={totals(s.receivable)} meta="Open invoices less unapplied credits" />}
            {c?.vendor !== false && <Tile label="We owe" value={totals(s.payable)} meta="Open bills less unapplied credits" />}
            {s.invoiced && <Tile label="Invoiced this financial year" value={money(s.invoiced)} meta="Less credit notes, GST included" />}
            {s.billed && <Tile label="Billed this financial year" value={money(s.billed)} meta="Less vendor credits, GST included" />}
          </div>
          {s.year && (
            <p className="fy-small fy-muted">
              As at {date(s.as_at)}; the financial year runs {date(s.year.from)} to {date(s.year.to)}.
            </p>
          )}
          {(s.receivable.length > 0 || s.payable.length > 0) && (
            <section className="fy-card" aria-labelledby={`${id}-ageing`}>
              <div className="fy-card__head">
                <h2 className="fy-h3" id={`${id}-ageing`}>
                  By days past due
                </h2>
              </div>
              <div className="fy-card__body fy-stack fy-stack--3">
                <Ageing rows={s.receivable} label="What they owe us" />
                <Ageing rows={s.payable as Row[]} label="What we owe them" />
              </div>
            </section>
          )}
          <section className="fy-card" aria-labelledby={`${id}-activity`}>
            <div className="fy-card__head">
              <h2 className="fy-h3" id={`${id}-activity`}>
                Activity
              </h2>
            </div>
            {s.timeline.length === 0 ? (
              <p className="fy-card__body fy-small fy-secondary">No documents yet.</p>
            ) : (
              <ul className="fy-list" aria-labelledby={`${id}-activity`}>
                {s.timeline.map((e) => {
                  const link = linkOf(e);
                  const name = `${kinds[e.type] ?? e.type}${e.number ? ` ${e.number}` : ""}`;
                  return (
                    <li key={e.id} className="fy-list__item">
                      <span className="fy-list__icon">
                        <Icon of={History} size={20} />
                      </span>
                      <span className="fy-list__main">
                        <span className="fy-list__title whitespace-normal">
                          {link ? (
                            <Link to={link.to} params={link.params}>
                              {name}
                            </Link>
                          ) : (
                            name
                          )}
                        </span>
                        <span className="fy-list__meta">
                          {e.date ? date(e.date) : "No date yet"} · {e.status}
                        </span>
                      </span>
                      {e.amount && <span className="fy-num">{money(e.amount)}</span>}
                    </li>
                  );
                })}
              </ul>
            )}
          </section>
        </>
      )}
    </div>
  );
}
