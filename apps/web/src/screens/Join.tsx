import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Globe, Hourglass, Plus, Trash2, UserPlus, X } from "lucide-react";
import { useEffect, useId, useState, type FormEvent } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { Refusal } from "../components/Refusal";
import { Button } from "../components/ui/button";
import { date, dateTime } from "../format/date";
import { helpText } from "../i18n";
import { guarded, useConfirm } from "../stepup";
import { Icon, problemOf, type ApiProblem } from "../ui";

// Joining by email domain in the web app (FR-ADM-08, PRD-001 rev 0.199): the offer a person signed in without an account
// sees, and the Owners' side on Members and roles: the requests and the claimed domains.

type JoinRequest = components["schemas"]["JoinRequestView"];

// AskToJoin is the sign-in page's offer to a person without an account whose verified email's domain an organisation
// claimed, through a provider that lets people join by it. It shows nothing when there is none.
export function AskToJoin() {
  const id = useId();
  // The sign-in page has no query client: the offer is read once, as the page reads the session.
  const [offer, setOffer] = useState<{ data: components["schemas"]["JoinOffer"] | null }>({ data: null });
  useEffect(() => {
    let live = true;
    void api
      .GET("/v1/session/join-offer")
      .then(({ data }) => live && setOffer({ data: data ?? null }))
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, []);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [asked, setAsked] = useState(false);
  const h = helpText("join.ask");
  if (!offer.data) return null;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/session/join-requests", { body: { display_name: name.trim() } });
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    setAsked(true);
  };

  if (asked)
    return (
      <section className="fy-card fy-card--pad fy-stack fy-stack--2" role="status">
        <h2 className="fy-h3">
          <Icon of={Hourglass} size={20} /> Your request waits for an Owner
        </h2>
        <p className="fy-secondary">An Owner of {offer.data.organisation_name} decides it. Once they approve it, sign in again to start, as an Employee.</p>
      </section>
    );
  return (
    <form className="fy-card fy-card--pad fy-stack fy-stack--3" onSubmit={submit} noValidate aria-labelledby={`${id}-title`}>
      <h2 className="fy-h3" id={`${id}-title`}>
        Ask to join {offer.data.organisation_name}
      </h2>
      <p className="fy-secondary">
        Your verified email {offer.data.email} is on {offer.data.domain}, which {offer.data.organisation_name} uses. Ask to join: an Owner approves it before you see
        anything, and you start as an Employee.
      </p>
      {problem && <Refusal problem={problem} title="The request was not sent" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-name`}>
          Your name
        </label>
        <input className="fy-input" id={`${id}-name`} value={name} maxLength={200} autoComplete="name" onChange={(e) => setName(e.target.value)} />
      </div>
      <Button type="submit" variant="primary" disabled={busy || !name.trim()} title={h.tooltip}>
        <Icon of={UserPlus} />
        {h.label}
      </Button>
    </form>
  );
}

// JoinRequests are the Members page's requests to join by email domain: pending ones with Approve and Reject (an Owner,
// after confirming it's them), decided ones below.
export function JoinRequests() {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const list = useQuery({
    queryKey: ["join-requests"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/members/join-requests", { params: { query: { page_size: 100 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [status, setStatus] = useState("");
  const approve = helpText("join.approve");
  const reject = helpText("join.reject");
  const items = list.data ?? [];
  const pending = items.filter((r) => r.status === "pending");
  if (!list.data || items.length === 0) return null;

  const decide = async (r: JoinRequest, yes: boolean) => {
    setBusy(true);
    setProblem(null);
    const params = { path: { request_id: r.id } };
    const out = await guarded(confirm, () =>
      yes ? api.POST("/v1/members/join-requests/{request_id}/approve", { params }) : api.POST("/v1/members/join-requests/{request_id}/reject", { params }),
    );
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    setStatus(yes ? `${r.display_name} is a member now, as an Employee.` : `${r.display_name}'s request is rejected.`);
    await Promise.all(["join-requests", "members"].map((k) => queries.invalidateQueries({ queryKey: [k] })));
  };

  return (
    <section className="fy-card" aria-labelledby="join-requests-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="join-requests-title">
          Requests to join
        </h2>
        {pending.length > 0 && <span className="fy-badge">{pending.length}</span>}
      </div>
      <p className="fy-small fy-secondary px-4" role="status">
        {status}
      </p>
      {problem && <Refusal problem={problem} title="The request was not decided" />}
      <ul className="fy-list" aria-labelledby="join-requests-title">
        {items.map((r) => (
          <li key={r.id} className="fy-list__item">
            <span className="fy-list__main">
              <span className="fy-list__title whitespace-normal">{r.display_name}</span>
              <span className="fy-list__meta whitespace-normal">
                {r.email} · with {r.provider} · asked {dateTime(r.requested_at)}
                {r.status !== "pending" && r.decided_at ? ` · ${r.status} ${date(r.decided_at)}` : ""}
              </span>
            </span>
            {r.status === "pending" ? (
              <span className="fy-row gap-2">
                <Button size="sm" variant="primary" disabled={busy} onClick={() => void decide(r, true)} aria-label={`${approve.label} ${r.display_name}`} title={approve.tooltip}>
                  <Icon of={Check} />
                  {approve.label}
                </Button>
                <Button size="sm" variant="ghost" disabled={busy} onClick={() => void decide(r, false)} aria-label={`${reject.label} ${r.display_name}`} title={reject.tooltip}>
                  <Icon of={X} />
                  {reject.label}
                </Button>
              </span>
            ) : (
              <span className={r.status === "approved" ? "fy-chip fy-chip--good" : "fy-chip fy-chip--neutral"}>{r.status === "approved" ? "Approved" : "Rejected"}</span>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}

// JoinDomains is the Members page's card of the email domains the organisation claimed for joining: an Owner adds or
// removes them after confirming it's them.
export function JoinDomains({ owner }: { owner: boolean }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const list = useQuery({
    queryKey: ["join-domains"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/organisation/join-domains", { params: { query: { page_size: 100 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const [domain, setDomain] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const claim = helpText("join.claim-domain");

  const change = async (d: string, add: boolean) => {
    setBusy(true);
    setProblem(null);
    const params = { path: { domain: d.trim().toLowerCase() } };
    const out = await guarded(confirm, () =>
      add ? api.PUT("/v1/organisation/join-domains/{domain}", { params }) : api.DELETE("/v1/organisation/join-domains/{domain}", { params }),
    );
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    if (add) setDomain("");
    await queries.invalidateQueries({ queryKey: ["join-domains"] });
  };

  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-title`}>
          Email domains for joining
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <p className="fy-small fy-secondary">
          People whose verified email is on one of these domains may ask to join, once a Super admin lets their sign-in provider. An Owner approves each request.
        </p>
        {problem && <Refusal problem={problem} title="The domains did not change" />}
        {(list.data ?? []).length > 0 && (
          <ul className="fy-list rounded-md border border-border" aria-labelledby={`${id}-title`}>
            {(list.data ?? []).map((d) => (
              <li key={d.domain} className="fy-list__item">
                <span className="fy-list__icon">
                  <Icon of={Globe} size={20} />
                </span>
                <span className="fy-list__main">
                  <span className="fy-list__title fy-mono">{d.domain}</span>
                  <span className="fy-list__meta">Claimed {date(d.claimed_at)}</span>
                </span>
                {owner && (
                  <Button size="sm" variant="ghost" disabled={busy} onClick={() => void change(d.domain, false)} aria-label={`Release ${d.domain}`}>
                    <Icon of={Trash2} />
                    <span className="fy-from-medium">Release</span>
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
        {owner && (
          <form
            className="fy-row fy-row--wrap gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (domain.trim()) void change(domain, true);
            }}
          >
            <label className="fy-sr" htmlFor={`${id}-domain`}>
              Email domain
            </label>
            <input className="fy-input fy-mono grow" id={`${id}-domain`} value={domain} placeholder="cognizhi.com" onChange={(e) => setDomain(e.target.value)} />
            <Button type="submit" size="sm" disabled={busy || !domain.trim()} title={claim.tooltip}>
              <Icon of={Plus} />
              {claim.label}
            </Button>
          </form>
        )}
      </div>
    </section>
  );
}
