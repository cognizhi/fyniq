import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Car, Download, HandCoins, Receipt, TriangleAlert } from "lucide-react";
import { useId, useState } from "react";
import { api } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { formatAmount } from "../../format/amount";
import { csvCell } from "../../format/csv";
import { date } from "../../format/date";
import { isZero } from "../../format/decimal";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";
import { CardLines } from "./CardLines";
import { money, StageChip, stages, type Advance, type Claim, type Stage } from "./claims";

type View = "claims" | "mileage" | "card" | "advances";

// MyClaims is My claims (docs/design/screens MyClaims and PersonalClaims; FR-AP-14, FR-AP-18, FR-UX-39): the member's
// own claims by stage with what the company still owes them, a returned claim's comment up front, the lines of the
// company cards they hold to complete, and their cash advances. It shows only the caller's own items: the API answers
// an Employee with theirs alone.
export function MyClaims() {
  const { session, organisation } = useSignedIn();
  const [view, setView] = useState<View>("claims");
  const [stage, setStage] = useState<Stage | "">("");
  const list = useInfiniteQuery({
    queryKey: ["expense-claims", "mine"],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/v1/expense-claims", { params: { query: { whose: "mine", page_size: 100, cursor: pageParam } } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const owed = useQuery({
    queryKey: ["expense-claims-owed"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/expense-claims/owed");
      if (error) throw error;
      return data;
    },
  });
  const cards = useQuery({
    queryKey: ["card-lines", "waiting"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/card-lines", { params: { query: { waiting: true, page_size: 100 } } });
      if (error) throw error;
      return data.items;
    },
  });
  const claims = list.data?.pages.flatMap((p) => p.items) ?? [];
  const shown = stage ? claims.filter((c) => c.stage === stage) : claims;
  const returned = claims.filter((c) => c.stage === "returned");
  const mine = owed.data?.people.find((p) => p.claimant.id === session.user_id);
  const add = helpText("claim.new");
  const base = organisation?.base_currency ?? "SGD";

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">For you</span>
          <h1 className="fy-h1">My claims</h1>
          <p className="fy-secondary">Costs you paid for the company, and what it still owes you.</p>
        </div>
        <div className="fy-pagehead__actions">
          {claims.length > 0 && (
            <button type="button" className="fy-btn fy-btn--ghost" onClick={() => download(claimsCSV(claims), "my-claims.csv")} title={helpText("claims.export").tooltip}>
              <Icon of={Download} />
              {helpText("claims.export").label}
            </button>
          )}
          <Link className="fy-btn fy-btn--secondary" to="/me/claims/new/mileage" title={helpText("claim.new-mileage").tooltip}>
            <Icon of={Car} />
            {helpText("claim.new-mileage").label}
          </Link>
          <Link className="fy-btn fy-btn--primary" to="/me/claims/new" title={add.tooltip}>
            <Icon of={Receipt} />
            {add.label}
          </Link>
        </div>
      </div>

      {returned.map((c) => (
        <div className="fy-banner fy-banner--warning" key={c.id}>
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <strong>{c.returned?.by?.name ? `${c.returned.by.name} returned your claim` : "Your claim was returned"}</strong>
            <span className="fy-small">
              {[c.number, c.title, c.total && money(c.total)].filter(Boolean).join(" · ")}
              {c.returned?.comment && ` · “${c.returned.comment}”`}
            </span>
          </div>
          <Link className="fy-btn fy-btn--secondary fy-btn--sm" to="/me/claims/$claimId" params={{ claimId: c.id }}>
            Fix it
          </Link>
        </div>
      ))}

      <section className="fy-card" aria-labelledby="owed-title">
        <div className="fy-card__body fy-stack fy-stack--3">
          <div className="fy-tile__label" id="owed-title" title={helpText("claims.owed").tooltip}>
            {helpText("claims.owed").label}
          </div>
          {owed.isError && <Refusal problem={problemOf(owed.error)} title="What the company owes you could not be read" />}
          {owed.data && (
            <div className="fy-row fy-row--wrap gap-3">
              <span className="fy-tile__value fy-num">
                <span className="fy-muted">{base} </span>
                {formatAmount(mine?.open.amount ?? owed.data.total.amount)}
              </span>
              <span className="fy-small fy-muted">
                {mine && mine.claims.length > 0 ? `${mine.claims.length} approved ${mine.claims.length === 1 ? "claim" : "claims"} not yet paid back` : "Nothing is owed to you now"}
              </span>
            </div>
          )}
        </div>
      </section>

      <nav className="fy-tabs" aria-label="Claim views">
        {(
          [
            ["claims", "Claims", claims.length],
            ["mileage", "Mileage", undefined],
            ["card", "Card lines", cards.data?.length ?? 0],
            ["advances", "Cash advances", undefined],
          ] as [View, string, number | undefined][]
        ).map(([v, label, count]) => (
          <button key={v} type="button" aria-current={view === v ? "page" : undefined} onClick={() => setView(v)}>
            {label}
            {count !== undefined && count > 0 && <span className="fy-badge fy-badge--quiet">{count}</span>}
          </button>
        ))}
      </nav>

      {view === "claims" && (
        <section className="fy-stack fy-stack--3" aria-label="Claims">
          {list.isError && <Refusal problem={problemOf(list.error)} title="Your claims could not be listed" />}
          {list.isPending && <p className="fy-secondary">Loading your claims…</p>}
          {claims.length > 0 && (
            <div className="fy-chipset" role="group" aria-label="Filter by stage">
              <button className="fy-filter" type="button" aria-pressed={stage === ""} onClick={() => setStage("")}>
                All {claims.length}
              </button>
              {stages
                .filter((s) => claims.some((c) => c.stage === s.stage))
                .map((s) => (
                  <button key={s.stage} className="fy-filter" type="button" aria-pressed={stage === s.stage} onClick={() => setStage(s.stage)}>
                    {s.label} {claims.filter((c) => c.stage === s.stage).length}
                  </button>
                ))}
            </div>
          )}
          {list.data && claims.length === 0 && (
            <div className="fy-empty fy-card fy-card--pad">
              <Icon of={Receipt} size={24} />
              <h2 className="fy-h3">No claims yet</h2>
              <p className="fy-secondary">Paid for something for the company? Claim it from the receipt, and the company pays you back once it is approved.</p>
              <Link className="fy-btn fy-btn--primary" to="/me/claims/new">
                <Icon of={Receipt} />
                {add.label}
              </Link>
            </div>
          )}
          {shown.length > 0 && <ClaimTable claims={shown} />}
          {list.hasNextPage && (
            <div>
              <button type="button" className="fy-btn fy-btn--secondary" onClick={() => void list.fetchNextPage()} disabled={list.isFetchingNextPage}>
                Show more
              </button>
            </div>
          )}
        </section>
      )}
      {view === "mileage" && <MileageLog />}
      {view === "card" && <CardLines />}
      {view === "advances" && <Advances />}
    </div>
  );
}

// claimsCSV is the member's claims as a CSV file (FR-UX-20): each amount a plain decimal with an ASCII "-", and text a
// spreadsheet would read as a formula made inert (csvCell).
export function claimsCSV(claims: Claim[]): string {
  const rows = [["Number", "Date", "Title", "Stage", "Amount", "Currency"].map((h) => csvCell(h))];
  for (const c of claims) {
    const label = stages.find((s) => s.stage === c.stage)?.label ?? c.stage;
    rows.push([csvCell(c.number ?? ""), csvCell(c.date ?? ""), csvCell(c.title || describe(c)), csvCell(label), c.total ? csvCell(c.total.amount, true) : "", csvCell(c.total?.currency ?? "")]);
  }
  return rows.map((r) => r.join(",")).join("\r\n") + "\r\n";
}

function download(text: string, name: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "text/csv;charset=utf-8" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

function ClaimTable({ claims }: { claims: Claim[] }) {
  return (
    <div className="fy-tablewrap">
      <table className="fy-table fy-table--cards" aria-label="Your claims">
        <thead>
          <tr>
            <th scope="col">Claim</th>
            <th scope="col">Number</th>
            <th scope="col">Date</th>
            <th scope="col">Stage</th>
            <th scope="col" className="fy-num">
              Amount
            </th>
          </tr>
        </thead>
        <tbody>
          {claims.map((c) => (
            <tr key={c.id}>
              <td data-card="title">
                <Link to="/me/claims/$claimId" params={{ claimId: c.id }}>
                  {c.title || describe(c)}
                </Link>
              </td>
              <td data-card="meta" className="fy-mono">
                {c.number}
              </td>
              <td data-card="meta">{date(c.date)}</td>
              <td data-card="status">
                <StageChip stage={c.stage} />
              </td>
              <td data-card="amount" className="fy-num">
                {c.total ? money(c.total) : "Not complete"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// describe names a claim without a title by its first line: the merchant, or a trip.
function describe(c: Claim): string {
  const first = c.lines[0];
  if (!first) return "Claim";
  const what = first.kind === "mileage" ? `Trip to ${first.to ?? "a client"}` : first.merchant || "Receipt";
  return c.lines.length > 1 ? `${what} and ${c.lines.length - 1} more` : what;
}

// MileageLog is the member's trips in a month (FR-AP-16's monthly mileage log): on drafts and claims alike, with the
// distance claimed and what each comes to at the rate on its date.
function MileageLog() {
  const id = useId();
  const [month, setMonth] = useState(new Date().toISOString().slice(0, 7));
  const q = useQuery({
    queryKey: ["mileage-log", month],
    enabled: /^\d{4}-\d{2}$/.test(month),
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/expense-claims/mileage-log", { params: { query: { month } } });
      if (error) throw error;
      return data;
    },
  });
  const trips = q.data?.trips ?? [];
  return (
    <section className="fy-stack fy-stack--3" aria-label="Mileage">
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-month`}>
          Month
        </label>
        <input className="fy-input" id={`${id}-month`} type="month" value={month} onChange={(e) => setMonth(e.target.value)} />
      </div>
      {q.isError && <Refusal problem={problemOf(q.error)} title="Your mileage log could not be read" />}
      {q.data && trips.length === 0 && <p className="fy-secondary">No trips in this month.</p>}
      {trips.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label="Your trips">
            <thead>
              <tr>
                <th scope="col">Trip</th>
                <th scope="col">Date</th>
                <th scope="col" className="fy-num">
                  Km claimed
                </th>
                <th scope="col" className="fy-num">
                  Amount
                </th>
                <th scope="col">Stage</th>
              </tr>
            </thead>
            <tbody>
              {trips.map((t, i) => (
                <tr key={`${t.claim_id}-${i}`}>
                  <td data-card="title">
                    <Link to="/me/claims/$claimId" params={{ claimId: t.claim_id }}>
                      {t.from} to {t.to}
                      {t.return ? " and back" : ""}
                    </Link>
                    <span className="fy-small fy-muted block">{[t.purpose, t.vehicle === "car" ? "Car" : "Motorcycle"].filter(Boolean).join(" · ")}</span>
                  </td>
                  <td data-card="meta">{date(t.date)}</td>
                  <td className="fy-num">{t.claimed_km}</td>
                  <td data-card="amount" className="fy-num">
                    {money(t.amount)}
                  </td>
                  <td data-card="status">
                    <StageChip stage={t.stage as Stage} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {q.data && trips.length > 0 && <p className="fy-small">The month's trips come to {money(q.data.total)}.</p>}
    </section>
  );
}

// Advances are the member's cash advances and their refunds (FR-AP-17): what is still open on each, which their next
// claim settles or they refund, with ⚠ when it is open past the claim deadline.
function Advances() {
  const { session } = useSignedIn();
  const q = useQuery({
    queryKey: ["cash-advances", "mine"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/cash-advances", { params: { query: { person_id: session.user_id, page_size: 100 } } });
      if (error) throw error;
      return data.items;
    },
  });
  const items: Advance[] = q.data ?? [];
  return (
    <section className="fy-stack fy-stack--3" aria-label="Cash advances">
      {q.isError && <Refusal problem={problemOf(q.error)} title="Your cash advances could not be listed" />}
      {q.data && items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={HandCoins} size={24} />
          <h2 className="fy-h3">No cash advances</h2>
          <p className="fy-secondary">When finance advances you money for a trip, it shows here. Your claims for the trip settle it, and you refund what is left.</p>
        </div>
      )}
      {items.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label="Your cash advances">
            <thead>
              <tr>
                <th scope="col">Advance</th>
                <th scope="col">Date</th>
                <th scope="col" className="fy-num">
                  Amount
                </th>
                <th scope="col" className="fy-num">
                  Still open
                </th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {items.map((a) => (
                <tr key={a.id}>
                  <td data-card="title">
                    <span className="fy-mono">{a.number}</span>
                    <span className="fy-small fy-muted block">{a.kind === "refund" ? `Refund of ${a.refunds?.number ?? "an advance"}` : a.notes || a.reference || "Cash advance"}</span>
                  </td>
                  <td data-card="meta">{date(a.date)}</td>
                  <td data-card="amount" className="fy-num">
                    {money(a.amount)}
                  </td>
                  <td className="fy-num">{a.open ? money(a.open) : ""}</td>
                  <td data-card="status">
                    {a.status === "reversed" ? (
                      <span className="fy-chip fy-chip--neutral">Voided</span>
                    ) : a.checks.length > 0 ? (
                      <span className="fy-chip fy-chip--warning" title={a.checks.map((c) => `${c.message}. ${c.fix ?? ""}`).join(" ")}>
                        <Icon of={TriangleAlert} />
                        Open past the deadline
                      </span>
                    ) : (
                      <span className="fy-chip fy-chip--neutral">{a.kind === "refund" ? "Refunded" : a.open && isZero(a.open.amount) ? "Settled" : "Open"}</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
