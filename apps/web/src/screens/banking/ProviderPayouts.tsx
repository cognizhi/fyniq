import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, CircleX, CreditCard, FileMinus, Hourglass, ReceiptText, TriangleAlert } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { BankTabs, makers } from "./BankAccounts";

type Account = components["schemas"]["PaymentProviderAccount"];
type Payout = components["schemas"]["PaymentProviderPayout"];
type Dispute = components["schemas"]["PaymentProviderDispute"];
type Event = components["schemas"]["PaymentProviderEvent"];
type Money = components["schemas"]["Money"];

// Who sees the dispute actions: the API still answers each with the right it needs (a draft journal is created under
// Journals, a credit settled under Invoices with the right to post), so a refusal is shown in plain words.
const settlers = [...makers, "controller", "owner"];

export const providerNames: Record<string, string> = { stripe: "Stripe", hitpay: "HitPay" };
export const providerName = (p: string) => providerNames[p] ?? p;

export const categoryWords: Record<components["schemas"]["PaymentProviderTxn"]["category"], string> = {
  charge: "Charges",
  refund: "Refunds",
  dispute: "Disputes",
  dispute_reversal: "Disputes won back",
  fee: "Fees",
  payout: "Payout",
  other: "Other",
};

const sameAmount = (a?: Money, b?: Money) => !!a && !!b && a.currency === b.currency && trimmed(a.amount) === trimmed(b.amount);
// trimmed compares decimal strings without adding anything up: "12.50" and "12.5000" are one amount.
const trimmed = (v: string) => {
  const t = v.includes(".") ? v.replace(/0+$/, "").replace(/\.$/, "") : v;
  return t === "-0" ? "0" : t;
};
const show = (m: Money) => `${formatAmount(m.amount)} ${m.currency}`;

// payoutChip says whether a payout and what it settles come to nothing in the clearing account (check C.2 #42): the answer is
// the server's, never added up here (FR-UX-22).
function PayoutChip({ p }: { p: Payout }) {
  if (p.nets_to_zero === true)
    return (
      <span className="fy-chip fy-chip--good">
        <Icon of={CircleCheck} />
        Nets to zero
      </span>
    );
  if (p.nets_to_zero === false)
    return (
      <span className="fy-chip fy-chip--critical">
        <Icon of={CircleX} />
        {p.difference ? `${formatAmount(p.difference.amount)} left` : "Does not net to zero"}
      </span>
    );
  return (
    <span className="fy-chip fy-chip--neutral">
      <Icon of={Hourglass} />
      Waiting for its bank line
    </span>
  );
}

const disputeWords: Record<Dispute["status"], string> = { open: "Open", won: "Won", written_off: "Written off", credited: "Credited" };

// ProviderPayouts is Banking › Payouts (docs/design/screens ProviderPayouts; FR-PSP-01, FR-PSP-04..06; p1-payment-providers
// task 4.6): each payment provider's clearing account against what the provider says it holds, each payout broken into the
// charges, refunds, disputes and fees it settles with the check that it comes to nothing, the disputes with the two ways to
// settle a lost one, and the events Fyniq parked for a person. Every figure is the server's; the screen adds nothing up.
export function ProviderPayouts() {
  const { session } = useSignedIn();
  const roles = session.roles ?? [];
  const mayResolve = roles.some((r) => makers.includes(r));
  const maySettle = roles.some((r) => settlers.includes(r));
  const queries = useQueryClient();
  const [provider, setProvider] = useState<string | undefined>();
  const [opened, setOpened] = useState<string | null>(null);
  const [writing, setWriting] = useState<Dispute | null>(null);
  const [crediting, setCrediting] = useState<Dispute | null>(null);
  const [resolving, setResolving] = useState<Event | null>(null);
  const h = helpText("banking.payouts");

  const accounts = useQuery({
    queryKey: ["psp", "accounts"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/payment-providers/accounts", { params: { query: { page_size: 100 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const payouts = useInfiniteQuery({
    queryKey: ["psp", "payouts", provider ?? ""],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/v1/payment-providers/payouts", { params: { query: { page_size: 50, cursor: pageParam, money_account_id: provider } } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const disputes = useQuery({
    queryKey: ["psp", "disputes"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/payment-providers/disputes", { params: { query: { page_size: 100 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const parked = useQuery({
    queryKey: ["psp", "events", "parked"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/payment-providers/events", { params: { query: { page_size: 50, status: "parked" } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const refresh = () => {
    for (const key of [["psp"], ["money-accounts"], ["bank-lines"]]) void queries.invalidateQueries({ queryKey: key });
  };

  const list = accounts.data ?? [];
  const rows = payouts.data?.pages.flatMap((p) => p.items ?? []) ?? [];
  const broken = rows.filter((p) => p.nets_to_zero === false);
  const open = rows.find((p) => p.id === opened) ?? rows[0];
  const names = new Map(list.map((a) => [a.money_account_id, `${providerName(a.provider)} · ${a.currency}`]));
  const parkedBy = (a: Account) => (parked.data ?? []).filter((e) => e.connector_id === a.connector_id).length;

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Banking</span>
          <h1 className="fy-h1">Payment provider payouts</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
      </div>
      <ReviewBanner screen="provider-payouts" />
      <BankTabs />
      {accounts.isError && <Refusal problem={problemOf(accounts.error)} title="The payment providers could not be listed" />}
      {broken.length > 0 && (
        <div className="fy-banner fy-banner--critical" role="alert">
          <Icon of={CircleX} size={20} />
          <div className="fy-banner__text">
            <strong>
              {broken.length === 1
                ? `${providerName(rowProvider(broken[0]!, list))} payout ${broken[0]!.payout_id} does not net to zero`
                : `${broken.length} payouts do not net to zero`}
            </strong>
            <span className="fy-small">
              {broken[0]!.missing ?? "Something the payout settles is not in the clearing account yet."} Open the payout to see which line is missing; the hourly sweep fetches what the provider
              has since reported.
            </span>
          </div>
          <button type="button" className="fy-btn fy-btn--primary fy-btn--sm" onClick={() => setOpened(broken[0]!.id)}>
            Review payout
          </button>
        </div>
      )}
      {accounts.data && list.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={CreditCard} size={24} />
          <h2 className="fy-h3">No payment provider is set up</h2>
          <p className="fy-secondary">
            Install the Stripe or HitPay connector, then set up its clearing account on Settings › Integrations. Its charges, fees and payouts are broken down here once the first sweep has run.
          </p>
          <Link className="fy-btn fy-btn--primary" to="/settings/integrations">
            Open integrations
          </Link>
        </div>
      )}
      {list.length > 0 && (
        <div className="fy-grid fy-grid--2 fy-stagger">
          {list.map((a) => (
            <ProviderCard key={a.money_account_id} a={a} parked={parkedBy(a)} />
          ))}
        </div>
      )}
      {list.length > 0 && (
        <section className="fy-card" aria-labelledby="po-title">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="po-title">
              Payouts
            </h2>
            {list.length > 1 && (
              <div className="fy-chipset" role="group" aria-label="Provider">
                <button type="button" className="fy-filter" aria-pressed={provider === undefined} onClick={() => setProvider(undefined)}>
                  All
                </button>
                {list.map((a) => (
                  <button key={a.money_account_id} type="button" className="fy-filter" aria-pressed={provider === a.money_account_id} onClick={() => setProvider(a.money_account_id)}>
                    {providerName(a.provider)}
                  </button>
                ))}
              </div>
            )}
          </div>
          {payouts.isError && <Refusal problem={problemOf(payouts.error)} title="The payouts could not be listed" />}
          {payouts.data && rows.length === 0 && <p className="fy-secondary fy-card__body">No payout has been reported yet. The sweep runs every hour.</p>}
          {rows.length > 0 && (
            <div className="fy-split">
              <div className="fy-tablewrap">
                <table className="fy-table fy-table--cards" aria-label="Payouts">
                  <thead>
                    <tr>
                      <th scope="col">Date</th>
                      <th scope="col">Payout</th>
                      <th scope="col" className="fy-num">
                        Amount
                      </th>
                      <th scope="col">Clearing</th>
                    </tr>
                  </thead>
                  <tbody>
                    {rows.map((p) => (
                      <tr key={p.id} aria-selected={open?.id === p.id || undefined}>
                        <td data-card="meta">{p.arrival_date ? date(p.arrival_date) : "Not arrived"}</td>
                        <td data-card="title">
                          <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" onClick={() => setOpened(p.id)} aria-label={`Open payout ${p.payout_id}`}>
                            {names.get(p.money_account_id) ?? "Provider"} <span className="fy-mono">{p.payout_id}</span>
                          </button>
                        </td>
                        <td data-card="amount" className="fy-num">
                          {show(p.amount)}
                        </td>
                        <td data-card="status">
                          <PayoutChip p={p} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                {payouts.hasNextPage && (
                  <button type="button" className="fy-btn fy-btn--secondary" onClick={() => void payouts.fetchNextPage()} disabled={payouts.isFetchingNextPage}>
                    Show more
                  </button>
                )}
              </div>
              {open && <PayoutDetail p={open} name={names.get(open.money_account_id) ?? "Provider"} />}
            </div>
          )}
        </section>
      )}
      {list.length > 0 && (
        <section className="fy-card" aria-labelledby="dp-title">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="dp-title">
              Disputes
            </h2>
          </div>
          {disputes.isError && <Refusal problem={problemOf(disputes.error)} title="The disputes could not be listed" />}
          {disputes.data && disputes.data.length === 0 && <p className="fy-secondary fy-card__body">No dispute is on a provider's clearing account.</p>}
          {disputes.data && disputes.data.length > 0 && (
            <div className="fy-tablewrap">
              <table className="fy-table fy-table--cards" aria-label="Disputes">
                <thead>
                  <tr>
                    <th scope="col">Dispute</th>
                    <th scope="col">Status</th>
                    <th scope="col" className="fy-num">
                      Disputed
                    </th>
                    <th scope="col" className="fy-num">
                      Held
                    </th>
                    <th scope="col">Settle a lost one</th>
                  </tr>
                </thead>
                <tbody>
                  {disputes.data.map((d) => (
                    <tr key={`${d.money_account_id}-${d.dispute_id}`}>
                      <td data-card="title">
                        <span className="fy-mono">{d.dispute_id}</span>
                        <span className="fy-small fy-muted block">{names.get(d.money_account_id)}</span>
                      </td>
                      <td data-card="status">
                        <span className={`fy-chip fy-chip--${d.status === "open" ? "warning" : "neutral"}`}>{disputeWords[d.status]}</span>
                        {d.status === "open" && !d.all_posted && <span className="fy-small fy-muted block">Some lines have not posted</span>}
                      </td>
                      <td data-card="amount" className="fy-num">
                        {show(d.disputed)}
                      </td>
                      <td data-card="amount" className="fy-num">
                        {show(d.held)}
                        {sameAmount(d.credited, { amount: "0", currency: d.credited.currency }) ? null : <span className="fy-small fy-muted block">{show(d.credited)} credited</span>}
                      </td>
                      <td>
                        {d.loss_document_id && (
                          <Link to="/accounting/journals/$journalId" params={{ journalId: d.loss_document_id }} className="fy-small">
                            Open the draft journal
                          </Link>
                        )}
                        {d.status === "open" && d.all_posted && maySettle && (
                          <span className="fy-row gap-2 fy-row--wrap">
                            <button type="button" className="fy-btn fy-btn--secondary fy-btn--sm" onClick={() => setCrediting(d)} aria-label={`Settle a credit note against dispute ${d.dispute_id}`}>
                              <Icon of={ReceiptText} />
                              Settle a credit note
                            </button>
                            <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" onClick={() => setWriting(d)} aria-label={`Write off dispute ${d.dispute_id}`}>
                              <Icon of={FileMinus} />
                              Write it off
                            </button>
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}
      {parked.data && parked.data.length > 0 && (
        <section className="fy-card" aria-labelledby="ev-title">
          <div className="fy-card__head">
            <h2 className="fy-h3" id="ev-title">
              Events waiting for a person
            </h2>
            <span className="fy-chip fy-chip--warning">
              <Icon of={TriangleAlert} />
              {parked.data.length} parked
            </span>
          </div>
          <div className="fy-tablewrap">
            <table className="fy-table fy-table--cards" aria-label="Parked events">
              <thead>
                <tr>
                  <th scope="col">Received</th>
                  <th scope="col">Event</th>
                  <th scope="col">Why it is parked</th>
                  <th scope="col">Deal with it</th>
                </tr>
              </thead>
              <tbody>
                {parked.data.map((e) => (
                  <tr key={e.id}>
                    <td data-card="meta">{date(e.received_at)}</td>
                    <td data-card="title">
                      {providerName(e.provider)} <span className="fy-mono">{e.event_type}</span>
                      {e.object_id && <span className="fy-small fy-muted block fy-mono">{e.object_id}</span>}
                    </td>
                    <td data-card="meta">{e.parked_reason ?? e.last_error ?? "Fyniq does not know this event"}</td>
                    <td>
                      {mayResolve && (
                        <button type="button" className="fy-btn fy-btn--secondary fy-btn--sm" onClick={() => setResolving(e)} aria-label={`Mark the ${e.event_type} event as dealt with`}>
                          Mark as dealt with
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="fy-small fy-muted fy-card__body">A parked event posts nothing. The books follow the provider's statement lines; mark it once you have looked at it.</p>
        </section>
      )}
      {writing && (
        <WriteOffDialog
          d={writing}
          onClose={() => setWriting(null)}
          onDone={() => {
            setWriting(null);
            refresh();
          }}
        />
      )}
      {crediting && (
        <CreditDialog
          d={crediting}
          onClose={() => setCrediting(null)}
          onDone={() => {
            setCrediting(null);
            refresh();
            void queries.invalidateQueries({ queryKey: ["invoices"] });
          }}
        />
      )}
      {resolving && (
        <ResolveDialog
          e={resolving}
          onClose={() => setResolving(null)}
          onDone={() => {
            setResolving(null);
            refresh();
          }}
        />
      )}
    </div>
  );
}

function rowProvider(p: Payout, accounts: Account[]): string {
  return accounts.find((a) => a.money_account_id === p.money_account_id)?.provider ?? "";
}

// ProviderCard is one provider's clearing account against what the provider says it still holds for the organisation
// (check C.2 #50), with how far the sweep has made it complete.
function ProviderCard({ a, parked }: { a: Account; parked: number }) {
  const agrees = a.provider_balance ? sameAmount(a.book_balance, a.provider_balance) : undefined;
  return (
    <section className="fy-card" aria-labelledby={`pv-${a.money_account_id}`}>
      <div className="fy-card__head">
        <span className="fy-list__icon fy-list__icon--brand">
          <Icon of={CreditCard} size={20} />
        </span>
        <div className="fy-grow fy-stack" style={{ gap: 0 }}>
          <h2 className="fy-h3" id={`pv-${a.money_account_id}`}>
            {providerName(a.provider)} · {a.currency}
          </h2>
          <span className="fy-small fy-muted">{a.money_account_name}</span>
        </div>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <div className="fy-row fy-row--wrap">
          {agrees === true && (
            <span className="fy-chip fy-chip--good">
              <Icon of={CircleCheck} />
              Clearing agrees
            </span>
          )}
          {agrees === false && (
            <span className="fy-chip fy-chip--warning">
              <Icon of={TriangleAlert} />
              Clearing differs
            </span>
          )}
          {a.swept_through && (
            <span className="fy-chip fy-chip--good">
              <Icon of={CircleCheck} />
              Complete to {date(a.swept_through)}
            </span>
          )}
          {a.sweep_error && (
            <span className="fy-chip fy-chip--critical">
              <Icon of={CircleX} />
              The sweep failed
            </span>
          )}
          {parked > 0 && (
            <span className="fy-chip fy-chip--warning">
              <Icon of={TriangleAlert} />
              {parked} {parked === 1 ? "event" : "events"} parked
            </span>
          )}
        </div>
        <table className="fy-table fy-table--compact" aria-label={`${providerName(a.provider)} balances`}>
          <tbody>
            <tr>
              <td>Clearing balance</td>
              <td className="fy-num">{a.book_balance ? formatAmount(a.book_balance.amount) : "Not known yet"}</td>
            </tr>
            <tr>
              <td>{providerName(a.provider)}'s balance, not yet paid out</td>
              <td className="fy-num">{a.provider_balance ? formatAmount(a.provider_balance.amount) : "Not known yet"}</td>
            </tr>
          </tbody>
        </table>
        {a.sweep_error && <span className="fy-small fy-muted">{a.sweep_error}</span>}
        <span className="fy-small fy-muted">
          Fees go to {a.fee_account_name}
          {a.disputes_account_name ? `, disputes to ${a.disputes_account_name}` : ""}
          {a.payout_account_name ? `; payouts land in ${a.payout_account_name}` : ""}.
        </span>
      </div>
    </section>
  );
}

// PayoutDetail breaks one payout into what it settles, by category, as the provider reports it, and says whether it comes to
// nothing in the clearing account.
function PayoutDetail({ p, name }: { p: Payout; name: string }) {
  const groups = Object.entries(categoryWords)
    .map(([category, words]) => ({ words, items: p.transactions.filter((t) => t.category === category) }))
    .filter((g) => g.items.length > 0);
  return (
    <aside className="fy-card" aria-label={`Payout ${p.payout_id}`}>
      <div className="fy-card__head">
        <div className="fy-grow fy-stack" style={{ gap: 0 }}>
          <h3 className="fy-h3">
            {name} <span className="fy-mono">{p.payout_id}</span>
          </h3>
          <span className="fy-small fy-muted">
            {p.arrival_date ? `Paid out ${date(p.arrival_date)}` : "Not arrived yet"}
            {p.automatic ? " · automatic" : ""}
          </span>
        </div>
        <PayoutChip p={p} />
      </div>
      <table className="fy-table fy-table--compact" aria-label={`What payout ${p.payout_id} settles`}>
        <thead>
          <tr>
            <th scope="col">What it settles</th>
            <th scope="col" className="fy-num">
              Gross
            </th>
            <th scope="col" className="fy-num">
              Fee
            </th>
            <th scope="col" className="fy-num">
              Net
            </th>
          </tr>
        </thead>
        <tbody>
          {groups.map((g) => (
            <GroupRows key={g.words} words={g.words} items={g.items} />
          ))}
          {groups.length === 0 && (
            <tr>
              <td colSpan={4} className="fy-secondary">
                The provider has reported nothing under this payout yet.
              </td>
            </tr>
          )}
        </tbody>
        <tfoot>
          <tr>
            <th scope="row" colSpan={3}>
              Payout to the bank
            </th>
            <td className="fy-num">{show(p.amount)}</td>
          </tr>
        </tfoot>
      </table>
      <div className="fy-card__body fy-stack fy-stack--3">
        {!p.payout_line_posted && <span className="fy-small fy-muted">Its line has not posted: the bank's statement line for it has not arrived, or has not been matched yet.</span>}
        {p.missing && (
          <p className="fy-small">
            <Icon of={TriangleAlert} /> {p.missing}
          </p>
        )}
      </div>
    </aside>
  );
}

function GroupRows({ words, items }: { words: string; items: Payout["transactions"] }) {
  return (
    <>
      <tr>
        <th scope="rowgroup" colSpan={4}>
          {words} ({items.length})
        </th>
      </tr>
      {items.map((t) => (
        <tr key={t.txn_id}>
          <td className="fy-sub">
            <span className="fy-mono">{t.txn_id}</span>
            {!t.posted && <span className="fy-small fy-muted"> · not posted yet</span>}
          </td>
          <td className="fy-num">{formatAmount(t.gross.amount)}</td>
          <td className="fy-num">{formatAmount(t.fee.amount)}</td>
          <td className="fy-num">{formatAmount(t.net.amount)}</td>
        </tr>
      ))}
    </>
  );
}

// WriteOffDialog prepares the draft journal that writes a lost dispute off (FR-PSP-04): it posts nothing; the draft is posted
// under Journals with its own approval. Above the expense limit the person confirms the invoice's revenue stays and says why.
function WriteOffDialog({ d, onClose, onDone }: { d: Dispute; onClose: () => void; onDone: () => void }) {
  const id = useId();
  const [reason, setReason] = useState("");
  const [accept, setAccept] = useState(false);
  const [missing, setMissing] = useState<string | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const go = async () => {
    if (d.over_limit && !accept) return setMissing("Confirm that the invoice's revenue stays as invoiced, or settle a credit note instead.");
    if (d.over_limit && !reason.trim()) return setMissing("Say why: the reason is kept with the draft journal.");
    setMissing(null);
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/payment-providers/disputes/{dispute_id}/loss", {
      params: { path: { dispute_id: d.dispute_id }, header: { "Idempotency-Key": newKey() } },
      body: { money_account_id: d.money_account_id, accept_overstated_revenue: d.over_limit ? accept : undefined, reason: reason.trim() || undefined },
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone();
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={`Write off dispute ${d.dispute_id}`}
      description={`Prepares a draft journal for ${show(d.held)}: Dr the dispute loss account, Cr disputes clearing. Nothing posts yet; you post the draft under Journals.`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void go()} disabled={busy}>
            Prepare the draft journal
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The draft journal was not prepared" />}
      {d.over_limit && (
        <p className="fy-banner fy-banner--warning fy-small" role="note">
          This is above the limit up to which a lost dispute is written off as an expense. If the invoice is known, settling a credit note reverses its revenue and GST instead [accountant to confirm].
        </p>
      )}
      {d.over_limit && (
        <label className="fy-row gap-2" htmlFor={`${id}-accept`}>
          <input id={`${id}-accept`} type="checkbox" checked={accept} onChange={(e) => setAccept(e.target.checked)} />
          The invoice's revenue stays as invoiced
        </label>
      )}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-reason`}>
          {d.over_limit ? "Why" : "Why (optional)"}
        </label>
        <input
          className="fy-input"
          id={`${id}-reason`}
          value={reason}
          maxLength={500}
          placeholder="Such as: the customer won the chargeback"
          onChange={(e) => setReason(e.target.value)}
          aria-invalid={missing ? true : undefined}
          aria-describedby={missing ? `${id}-error` : undefined}
        />
        {missing && (
          <span className="fy-error" id={`${id}-error`}>
            {missing}
          </span>
        )}
      </div>
    </Dialog>
  );
}

// CreditDialog settles an issued credit note's credit against a lost dispute (dispute_settlement@1): the invoice is credited
// first, with its own approval; this moves the credit onto the money the dispute holds.
function CreditDialog({ d, onClose, onDone }: { d: Dispute; onClose: () => void; onDone: () => void }) {
  const id = useId();
  const [credit, setCredit] = useState("");
  const [missing, setMissing] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const notes = useQuery({
    queryKey: ["invoices", "credit-notes", "open"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/invoices", { params: { query: { kind: "credit_note", status: "posted", page_size: 100 } } });
      if (error) throw error;
      return (data.items ?? []).filter((n) => n.open && n.open.currency === d.disputed.currency && !sameAmount(n.open, { amount: "0", currency: n.open.currency }));
    },
  });
  const go = async () => {
    if (!credit) return setMissing(true);
    setMissing(false);
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/payment-providers/disputes/{dispute_id}/credit-settlement", {
      params: { path: { dispute_id: d.dispute_id }, header: { "Idempotency-Key": newKey() } },
      body: { money_account_id: d.money_account_id, credit_note_id: credit },
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone();
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={`Settle a credit note against dispute ${d.dispute_id}`}
      description={`Issue the credit note for the disputed invoice first. Its credit, up to the ${show(d.disputed)} disputed, then clears against the money the dispute holds: Dr accounts receivable, Cr disputes clearing. The dispute fee stays held, for you to write off.`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void go()} disabled={busy}>
            Settle the credit
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The credit was not settled" />}
      {notes.isError && <Refusal problem={problemOf(notes.error)} title="The credit notes could not be listed" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-credit`}>
          Credit note
        </label>
        <select
          className="fy-select"
          id={`${id}-credit`}
          value={credit}
          onChange={(e) => setCredit(e.target.value)}
          aria-invalid={missing || undefined}
          aria-describedby={missing ? `${id}-error` : undefined}
        >
          <option value="">Choose an issued credit note</option>
          {(notes.data ?? []).map((n) => (
            <option key={n.id} value={n.id}>
              {n.number ?? "Credit note"} · {n.customer?.name ?? "No customer"} · {n.open ? show(n.open) : ""} left
            </option>
          ))}
        </select>
        {missing && (
          <span className="fy-error" id={`${id}-error`}>
            Choose the credit note that credits the disputed invoice.
          </span>
        )}
        {notes.data && notes.data.length === 0 && <span className="fy-small fy-muted">No issued credit note has a credit left. Credit the invoice under Sales first.</span>}
      </div>
    </Dialog>
  );
}

// ResolveDialog marks a parked event as dealt with, with what the person did or why nothing is needed: it posts and fetches
// nothing; the reason goes in the audit trail.
function ResolveDialog({ e, onClose, onDone }: { e: Event; onClose: () => void; onDone: () => void }) {
  const id = useId();
  const [reason, setReason] = useState("");
  const [missing, setMissing] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const go = async () => {
    if (!reason.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/payment-providers/events/{event_id}/resolution", {
      params: { path: { event_id: e.id }, header: { "Idempotency-Key": newKey() } },
      body: { reason: reason.trim() },
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone();
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title="Mark it as dealt with"
      description="This posts and fetches nothing: the books follow the provider's statement lines. Your reason is kept in the audit trail."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void go()} disabled={busy}>
            Mark as dealt with
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The event was not marked" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-reason`}>
          What you did, or why nothing is needed
        </label>
        <input
          className="fy-input"
          id={`${id}-reason`}
          value={reason}
          maxLength={500}
          placeholder="Such as: a test event from the provider's dashboard"
          onChange={(ev) => setReason(ev.target.value)}
          aria-invalid={missing || undefined}
          aria-describedby={missing ? `${id}-error` : undefined}
        />
        {missing && (
          <span className="fy-error" id={`${id}-error`}>
            Say what you did or why nothing is needed: it is kept with the event.
          </span>
        )}
      </div>
    </Dialog>
  );
}
