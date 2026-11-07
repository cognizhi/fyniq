import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CalendarClock, Plus, ScrollText, TriangleAlert } from "lucide-react";
import { Refusal } from "../../components/Refusal";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";
import { EquityTabs } from "./Holders";
import { count, drafters, listGrants, poolOf, RecordStatus, vestingText, type OptionGrant, type OptionPool as Pool } from "./optionWords";

// days is the whole days from one ISO day to another.
function days(from: string, to: string): number {
  return Math.round((Date.parse(to) - Date.parse(from)) / 86_400_000);
}

// comingUp are the next 30 days' vesting and expiries of the recorded grants: a cliff or monthly vesting, and a grant
// expiring with options vested and not exercised (a deadline its holder sees too).
function comingUp(grants: OptionGrant[], today: string): { key: string; text: string; when: string }[] {
  const out: { key: string; text: string; when: string; on: string }[] = [];
  for (const g of grants.filter((x) => x.status === "recorded")) {
    if (g.next_vests_on && days(today, g.next_vests_on) <= 30) {
      out.push({ key: `${g.id}-vest`, on: g.next_vests_on, text: `${g.holder_name}: ${count(g.next_vests)} vest on ${date(g.next_vests_on)}`,
        when: `${days(today, g.next_vests_on)} days` });
    }
    if (g.exercisable !== "0" && days(today, g.expires_on) >= 0 && days(today, g.expires_on) <= 30) {
      out.push({ key: `${g.id}-expiry`, on: g.expires_on, text: `${g.holder_name}: ${count(g.exercisable)} vested options lapse on ${date(g.expires_on)} unless exercised`,
        when: `${days(today, g.expires_on)} days` });
    }
  }
  return out.sort((a, b) => a.on.localeCompare(b.on));
}

// OptionPool is Equity › Option pool (docs/design/screens OptionPool; FR-EQ-19, Expert): the pool by resolution with
// what is granted, given back, exercised and available; every grant with its vesting and standing; the vesting and
// expiries coming up; and the resolutions that set the pool.
export function OptionPool() {
  const { session } = useSignedIn();
  const pool = useQuery({ queryKey: ["option-pool"], queryFn: poolOf });
  const grants = useQuery({ queryKey: ["option-grants"], queryFn: listGrants });
  const mayDraft = (session.roles ?? []).some((r) => drafters.includes(r));
  const inForce = pool.data?.resolutions.find((r) => r.in_force);
  const all = grants.data ?? [];
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Equity</span>
          <h1 className="fy-h1" title={helpText("equity.options").tooltip}>
            Option pool and grants
          </h1>
          <p className="fy-secondary">
            {inForce
              ? `${count(inForce.size)} shares set aside for options by ${inForce.resolution_ref} of ${date(inForce.resolved_on)}.`
              : "No pool is set yet: record the shareholders' resolution that sets aside shares for options."}
          </p>
        </div>
        {mayDraft && (
          <div className="fy-pagehead__actions">
            <Link className="fy-btn fy-btn--secondary" to="/equity/options/resolutions/new" title={helpText("pool-resolution.draft").tooltip}>
              <Icon of={ScrollText} />
              Record a resolution
            </Link>
            {inForce && (
              <Link className="fy-btn fy-btn--primary" to="/equity/options/grants/new" title={helpText("option-grant.draft").tooltip}>
                <Icon of={Plus} />
                New grant
              </Link>
            )}
          </div>
        )}
      </div>
      <EquityTabs current="options" />
      {(pool.isError || grants.isError) && <Refusal problem={problemOf(pool.error ?? grants.error)} title="The option pool could not be shown" />}
      {pool.data && <PoolCard p={pool.data} />}
      {grants.data && (
        <section className="fy-card" aria-labelledby="grants-title">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="grants-title">
              Grants <span className="fy-muted fy-num">{all.length}</span>
            </h2>
          </div>
          {all.length === 0 ? (
            <p className="fy-card__body fy-small fy-muted">No grant yet. Once the pool is set, an Accountant drafts each grant with its letter.</p>
          ) : (
            <div className="fy-tablewrap" style={{ border: 0, borderRadius: 0 }}>
              <table className="fy-table fy-table--cards" aria-label="Grants">
                <thead>
                  <tr>
                    <th scope="col">Holder</th>
                    <th scope="col" className="fy-num">
                      Options
                    </th>
                    <th scope="col" className="fy-num">
                      Price
                    </th>
                    <th scope="col">Vesting</th>
                    <th scope="col">Expiry</th>
                    <th scope="col" className="fy-num">
                      Vested
                    </th>
                    <th scope="col" className="fy-num">
                      Outstanding
                    </th>
                    <th scope="col">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {all.map((g) => (
                    <tr key={g.id}>
                      <td data-card="title">
                        <Link to="/equity/options/grants/$grantId" params={{ grantId: g.id }}>
                          {g.holder_name}
                        </Link>
                        <div className="fy-small fy-muted">Granted {date(g.granted_on)}</div>
                      </td>
                      <td data-card="amount" className="fy-num">
                        {count(g.number)}
                      </td>
                      <td className="fy-num">
                        {g.exercise_price} {g.currency}
                      </td>
                      <td>{vestingText(g)}</td>
                      <td>{date(g.expires_on)}</td>
                      <td className="fy-num">{count(g.vested)}</td>
                      <td className="fy-num">{count(g.outstanding)}</td>
                      <td data-card="meta">
                        <RecordStatus status={g.status} asked={g.approval_id} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}
      <div className="fy-split">
        {pool.data && grants.data && <ComingUp grants={all} today={pool.data.at} />}
        <div className="fy-stack fy-stack--4">
          {pool.data && pool.data.resolutions.length > 0 && <Resolutions p={pool.data} />}
          <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby="p2-title">
            <h2 className="fy-h3" id="p2-title">
              Expense and IR8A <span className="fy-chip fy-chip--neutral">Coming in P2</span>
            </h2>
            <p className="fy-small fy-secondary">
              Share-based payment expense schedules from each grant's fair value, and the employee share data for IR8A Appendix 8B, are planned
              for a later release. The grants recorded now are what they will use.
            </p>
          </section>
          <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby="employees-title">
            <h2 className="fy-h3" id="employees-title">
              What employees see
            </h2>
            <p className="fy-small fy-secondary">Each employee sees only their own grant, vesting and exercises under My shares and options. Nobody else's.</p>
          </section>
        </div>
      </div>
    </div>
  );
}

// PoolCard is the pool on its day: its size, what the grants took and gave back, what is outstanding, and what is left.
function PoolCard({ p }: { p: Pool }) {
  const full = p.size !== "0" && p.unallocated === "0";
  const rows: [string, string][] = [
    ["Size by resolution", count(p.size)],
    ["Granted, all time", count(p.granted)],
    ["Forfeited or expired, back in the pool", count(p.returned)],
    ["Exercised, now shares", count(p.exercised)],
    ["Outstanding", count(p.outstanding)],
    ["Vested and not exercised", count(p.exercisable)],
  ];
  return (
    <section className="fy-card" aria-labelledby="pool-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="pool-title">
          The pool · <span className="fy-num">{count(p.unallocated)}</span> available
        </h2>
        {full && (
          <span className="fy-chip fy-chip--warning">
            <Icon of={TriangleAlert} />
            Pool exhausted
          </span>
        )}
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <dl className="fy-dl">
          {rows.map(([k, v]) => (
            <div key={k}>
              <dt>{k}</dt>
              <dd className="fy-num">{v}</dd>
            </div>
          ))}
        </dl>
        <p className="fy-small fy-secondary">
          {count(p.size)} − {count(p.granted)} granted + {count(p.returned)} given back = {count(p.unallocated)} available. A grant larger than that is
          refused until the shareholders enlarge the pool; exercised options never return to it.
        </p>
      </div>
    </section>
  );
}

// ComingUp lists the next 30 days' vesting and expiries.
function ComingUp({ grants, today }: { grants: OptionGrant[]; today: string }) {
  const items = comingUp(grants, today);
  return (
    <section className="fy-card" aria-labelledby="coming-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="coming-title">
          Coming up
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--2">
        {items.length === 0 && <p className="fy-small fy-muted">Nothing vests or expires in the next 30 days.</p>}
        {items.map((x) => (
          <p key={x.key} className="fy-row justify-between gap-2 fy-small">
            <span>
              <Icon of={CalendarClock} /> {x.text}
            </span>
            <span className="fy-muted fy-num">{x.when}</span>
          </p>
        ))}
        <p className="fy-small fy-muted">A grant within 30 days of expiry with vested options left shows a deadline here and in the holder's own view.</p>
      </div>
    </section>
  );
}

// Resolutions are the resolutions that set the pool, the one in force marked.
function Resolutions({ p }: { p: Pool }) {
  return (
    <section className="fy-card" aria-labelledby="resolutions-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="resolutions-title">
          Resolutions
        </h2>
      </div>
      <ul className="fy-card__body fy-stack fy-stack--2">
        {p.resolutions.map((r) => (
          <li key={r.id} className="fy-row justify-between gap-2">
            <Link to="/equity/options/resolutions/$resolutionId" params={{ resolutionId: r.id }}>
              {count(r.size)} from {date(r.resolved_on)}
            </Link>
            <span className="fy-row gap-2">
              {r.in_force && <span className="fy-chip fy-chip--good">In force</span>}
              <RecordStatus status={r.status} asked={r.approval_id} />
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
}
