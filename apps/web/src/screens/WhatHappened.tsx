import { Link } from "@tanstack/react-router";
import { ArrowRight } from "lucide-react";
import { useId, useState } from "react";
import type { Session } from "../api/client";
import { guideLink } from "../help/links";
import { helpText } from "../i18n";
import { nav, routes } from "../registry";
import { mayOpen } from "../shell/navigation";
import { useSignedIn } from "../session";
import { Icon } from "../ui";

// A thing that happened, where it is recorded, and what to do there (the workbook's Start Here).
export interface Happening {
  id: string;
  what: string; // as the person would say it
  to: string; // the page that records it: a registry route
  place: string;
  how: string;
}

export const happenings: readonly Happening[] = [
  { id: "customer-paid", what: "A customer paid us", to: "/banking/categorise", place: "Banking › Categorise", how: "Accept the suggestion that names the invoice it pays, or, in Expert mode, match the line to the invoice yourself." },
  { id: "bill-customer", what: "We sold something and need to bill the customer", to: "/sales/invoices/new", place: "Sales › New invoice", how: "Enter an invoice. Fyniq follows what the customer still owes until the bank line that pays it is matched." },
  { id: "supplier-bill", what: "A supplier sent us a bill", to: "/purchases/bills/new", place: "Purchases › New bill", how: "Enter the bill with its lines and GST: Fyniq then follows what the company owes the supplier." },
  { id: "paid-supplier", what: "We paid a supplier", to: "/banking/categorise", place: "Banking › Categorise", how: "Accept the suggestion that names the bill it pays, or, in Expert mode, match the line to the bill yourself." },
  { id: "paid-personally", what: "I paid a company cost with my own money", to: "/me/claims/new", place: "My claims › New claim", how: "Make an expense claim with the receipt. A director or shareholder may keep it as a loan to the company instead of being paid back." },
  { id: "bought-equipment", what: "The company bought equipment, such as a laptop", to: "/purchases/bills/new", place: "Purchases › New bill", how: "Enter the bill on an asset account. It then shows under Fixed assets › Not in the register yet, where it is registered as an asset." },
  { id: "founder-lent", what: "A founder lent the company money", to: "/banking/categorise", place: "Banking › Categorise", how: "Categorise the bank line to the director's account: Fyniq asks whose. Money I put in then shows it." },
  { id: "bank-fee", what: "The bank charged a fee or paid interest", to: "/banking/categorise", place: "Banking › Categorise", how: "Choose its category. A bank rule can code the same line each month from then on." },
  { id: "transfer", what: "Money moved between two of our accounts", to: "/banking/categorise", place: "Banking › Categorise", how: "In Expert mode, mark the line as a transfer: Fyniq matches it to the other account's line, or keeps it in transit until that line arrives." },
];

// happeningsFor are the things a person can record: those whose page is part of their access (FR-UX-39, FR-UX-40).
export function happeningsFor(session: Pick<Session, "roles"> & { super_admin?: boolean }): Happening[] {
  return happenings.filter((h) => mayOpen(nav, session, h.to));
}

// WhatHappened is "What happened? Where to record it" (FR-UX-11; docs/design/screens Setup, from the workbook's Start
// Here): pick what happened, and it says where Fyniq records it and what to do there, with the way to that page and to
// the guide's page about it.
export function WhatHappened() {
  const { session } = useSignedIn();
  const id = useId();
  const list = happeningsFor(session);
  const [chosen, setChosen] = useState("");
  const h = helpText("home.what-happened");
  const picked = list.find((x) => x.id === chosen);
  if (list.length === 0) return null;
  const guide = picked && routes.find((r) => r.path === picked.to)?.guide;
  const learn = guide ? guideLink(guide) : undefined;
  return (
    <section className="fy-card fy-card--pad fy-stack fy-stack--3" aria-labelledby={`${id}-title`}>
      <h2 className="fy-h3" id={`${id}-title`} title={h.tooltip}>
        {h.label}
      </h2>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-what`}>
          Something happened
        </label>
        <select className="fy-select" id={`${id}-what`} value={chosen} onChange={(e) => setChosen(e.target.value)}>
          <option value="">Choose what happened</option>
          {list.map((x) => (
            <option key={x.id} value={x.id}>
              {x.what}
            </option>
          ))}
        </select>
      </div>
      {picked && (
        <div className="fy-plain" role="status">
          <Icon of={ArrowRight} />
          <span className="fy-stack fy-stack--1">
            <span>
              Record it in <Link to={picked.to}>{picked.place}</Link>. {picked.how}
            </span>
            {learn && (
              <Link className="fy-small" to="/help/$key" params={{ key: learn.to.replace(/^\/help\//, "") }} hash={learn.hash}>
                How it works, in the guide
              </Link>
            )}
          </span>
        </div>
      )}
    </section>
  );
}
