import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Link2, Trash2 } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Made = components["schemas"]["PortalLinkMade"];

const words: Record<string, string> = { live: "Live", revoked: "Revoked", expired: "Expired" };

// A customer's portal links (FR-AR-14): made by a person with the right to post on Invoices, shown once with the address to send,
// and revoked at once when they should stop working. Fyniq keeps only a hash of each token, so a link cannot be shown again.
export function PortalLinksCard({ contactId, mayMake }: { contactId: string; mayMake: boolean }) {
  const id = useId();
  const queries = useQueryClient();
  const key = ["portal-links", contactId];
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [label, setLabel] = useState("");
  const [made, setMade] = useState<Made | null>(null);
  const [copied, setCopied] = useState(false);
  const links = useQuery({
    queryKey: key,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/contacts/{contact_id}/portal-links", { params: { path: { contact_id: contactId } } });
      if (error) throw error;
      return data.links;
    },
  });
  const make = async () => {
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/contacts/{contact_id}/portal-links", { params: { path: { contact_id: contactId } }, body: { label: label.trim() || undefined } });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setMade(out.data);
    setCopied(false);
    setLabel("");
    await queries.invalidateQueries({ queryKey: key });
  };
  const revoke = async (linkId: string) => {
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/portal-links/{link_id}/revoke", { params: { path: { link_id: linkId }, header: { "Idempotency-Key": newKey() } } });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: key });
  };
  const address = made ? `${window.location.origin}${made.path}` : "";
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(address);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };
  const make$ = helpText("contact.portal-link");
  const revoke$ = helpText("contact.portal-revoke");
  return (
    <section className="fy-card" aria-labelledby={`${id}-portal`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-portal`}>
          Customer portal
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <p className="fy-secondary">A private link where this customer sees what they owe, pays where a payment link exists, and downloads invoices and statements. Anyone who holds the link sees what the customer sees.</p>
        {problem && <Refusal problem={problem} title="The portal link was not made" />}
        {made && (
          <div className="fy-banner fy-banner--info" role="status">
            <div className="fy-banner__text">
              <span className="fy-strong">Copy it now: it is shown only once</span>
              <div className="fy-row fy-row--wrap gap-2">
                <input className="fy-input" aria-label="The portal link" readOnly value={address} onFocus={(e) => e.currentTarget.select()} />
                <Button size="sm" variant="secondary" type="button" onClick={() => void copy()}>
                  <Icon of={Copy} />
                  {copied ? "Copied" : "Copy"}
                </Button>
              </div>
              <span className="fy-small">Send it to the customer yourself. It works until {date(made.expires_at.slice(0, 10))}.</span>
            </div>
          </div>
        )}
        {mayMake && (
          <div className="fy-row fy-row--wrap gap-2">
            <input className="fy-input" aria-label="A note about who it is for" value={label} maxLength={200} placeholder="Such as: sent to ap@acme.example" onChange={(e) => setLabel(e.target.value)} />
            <Button type="button" variant="secondary" onClick={() => void make()} disabled={busy} title={make$.tooltip}>
              <Icon of={Link2} />
              {make$.label}
            </Button>
          </div>
        )}
        {links.isError && <Refusal problem={problemOf(links.error)} title="The links could not be read" />}
        {links.data && links.data.length > 0 && (
          <ul className="fy-list">
            {links.data.map((l) => (
              <li className="fy-list__item" key={l.id}>
                <span className="fy-list__main">
                  <span className="fy-list__title">
                    {words[l.status] ?? l.status}
                    {l.label ? ` · ${l.label}` : ""}
                  </span>
                  <span className="fy-list__meta">
                    Made {date(l.created_at.slice(0, 10))}, works until {date(l.expires_at.slice(0, 10))}
                    {l.first_opened_at ? `, first opened ${date(l.first_opened_at.slice(0, 10))}` : ", not opened yet"}
                  </span>
                </span>
                {mayMake && l.status === "live" && (
                  <Button size="sm" variant="ghost" type="button" onClick={() => void revoke(l.id)} disabled={busy} title={revoke$.tooltip}>
                    <Icon of={Trash2} />
                    {revoke$.label}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  );
}
