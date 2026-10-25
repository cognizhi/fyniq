import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, Coins, CreditCard, FileQuestion, FileSpreadsheet, Info, Landmark, Mail, Plug, Plus, TriangleAlert, Upload, Wallet, type LucideIcon } from "lucide-react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { formatAmount } from "../../format/amount";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf } from "../../ui";

export type MoneyAccount = components["schemas"]["MoneyAccount"];
type CashPosition = components["schemas"]["CashPosition"];

// makers are the roles that add money accounts and import statements: C on "Bank transactions, categorise" (Appendix D).
export const makers = ["bookkeeper", "accountant"];

export const kinds: Record<MoneyAccount["kind"], { words: string; icon: LucideIcon }> = {
  bank: { words: "Bank account", icon: Landmark },
  card: { words: "Card", icon: CreditCard },
  wallet: { words: "Wallet", icon: Wallet },
  payment_gateway: { words: "Payment gateway", icon: Plug },
  cash: { words: "Cash", icon: Coins },
};

// SourceLine says where an account's data comes from now: statement files when it has no source of its own.
export function SourceLine({ account }: { account: MoneyAccount }) {
  const s = account.source;
  if (s?.kind === "email")
    return (
      <span className="fy-row gap-2">
        <Icon of={Mail} />
        Bank reports by email
      </span>
    );
  if (s?.kind === "connector")
    return (
      <span className="fy-row gap-2">
        <Icon of={Plug} />A connector syncs it
      </span>
    );
  return (
    <span className="fy-row gap-2">
      <Icon of={FileSpreadsheet} />
      Statement files you import
    </span>
  );
}

// FreshnessChip says how up to date an account's data is (FR-BNK-16): a gap is ⚠ in both modes, since it is safety
// information (§7.8); colour never carries the meaning alone.
export function FreshnessChip({ account }: { account: MoneyAccount }) {
  const f = account.freshness;
  if (f.open_gaps > 0)
    return (
      <span className="fy-chip fy-chip--warning">
        <Icon of={TriangleAlert} />
        {f.open_gaps === 1 ? "A gap in its statements" : `${f.open_gaps} gaps in its statements`}
      </span>
    );
  if (!f.last_import)
    return (
      <span className="fy-chip fy-chip--neutral">
        <Icon of={FileQuestion} />
        No statements yet
      </span>
    );
  return (
    <span className="fy-chip fy-chip--good">
      <Icon of={CircleCheck} />
      Complete to {date(f.complete_to)}
    </span>
  );
}

// BankTabs are Banking's pages; each later page joins as it is built.
export function BankTabs() {
  return (
    <nav className="fy-tabs" aria-label="Banking">
      <Link to="/banking" activeOptions={{ exact: true }}>
        Accounts
      </Link>
      <Link to="/banking/categorise" activeOptions={{ exact: true, includeSearch: false }}>
        Categorise
      </Link>
    </nav>
  );
}

// BankAccounts is Banking › Accounts (docs/design/screens BankAccounts; FR-BNK-01, FR-BNK-13, FR-BNK-16;
// p1-bank-import-reconcile task 10.1): the cash position, then each money account with its balance, how up to date
// its data is and how many of its lines wait. Figures come only from the server (FR-UX-22): nothing is added up here.
export function BankAccounts() {
  const { session } = useSignedIn();
  const accounts = useInfiniteQuery({
    queryKey: ["money-accounts"],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/v1/money-accounts", { params: { query: { page_size: 100, cursor: pageParam } } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const cash = useQuery({
    queryKey: ["cash-position"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/cash-position", {});
      if (error) throw error;
      return data;
    },
  });
  const items = accounts.data?.pages.flatMap((p) => p.items ?? []) ?? [];
  const may = (session.roles ?? []).some((r) => makers.includes(r));
  const add = helpText("bank-account.new");
  const waiting = items.reduce((n, a) => n + a.waiting, 0);
  const balances = new Map((cash.data?.accounts ?? []).map((a) => [a.money_account_id, a]));

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Banking</span>
          <h1 className="fy-h1">Bank accounts</h1>
          <p className="fy-secondary">Every money account, how up to date its data is, and what still needs a category.</p>
        </div>
        {may && (
          <div className="fy-pagehead__actions">
            <Link className="fy-btn fy-btn--ghost" to="/banking/accounts/new" title={add.tooltip}>
              <Icon of={Plus} />
              {add.label}
            </Link>
            <Link className="fy-btn fy-btn--primary" to="/banking/import" title={helpText("banking.import").tooltip}>
              <Icon of={Upload} />
              Import statement
            </Link>
          </div>
        )}
      </div>
      <ReviewBanner screen="bank-accounts" />
      <BankTabs />
      {cash.data && <CashCard position={cash.data} />}
      {cash.isError && <Refusal problem={problemOf(cash.error)} title="The cash position could not be read" />}
      {accounts.isError && <Refusal problem={problemOf(accounts.error)} title="The money accounts could not be listed" />}
      {accounts.isPending && <p className="fy-secondary">Loading the money accounts…</p>}
      {accounts.data && items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={Landmark} size={24} />
          <h2 className="fy-h3">No money accounts yet</h2>
          <p className="fy-secondary">Add the company's bank accounts, cards and wallets, then import their statements or let the bank's reports arrive by email.</p>
          {may ? (
            <Link className="fy-btn fy-btn--primary" to="/banking/accounts/new">
              <Icon of={Plus} />
              {add.label}
            </Link>
          ) : (
            <p className="fy-small fy-secondary">A Bookkeeper or an Accountant adds money accounts.</p>
          )}
        </div>
      )}
      {items.length > 0 && (
        <section className="fy-stack fy-stack--3" aria-labelledby="money-accounts">
          <div className="fy-section__head">
            <h2 className="fy-h2" id="money-accounts">
              Money accounts
            </h2>
            <span className="fy-small fy-muted">
              {items.length === 1 ? "1 account" : `${items.length} accounts`} · {waiting === 1 ? "1 line waits" : `${waiting} lines wait`}
            </span>
          </div>
          <div className="fy-grid fy-grid--2 fy-stagger">
            {items.map((a) => (
              <AccountCard key={a.id} account={a} balance={balances.get(a.id)} may={may} />
            ))}
          </div>
        </section>
      )}
      {accounts.hasNextPage && (
        <div>
          <button type="button" className="fy-btn fy-btn--secondary" onClick={() => void accounts.fetchNextPage()} disabled={accounts.isFetchingNextPage}>
            Show more
          </button>
        </div>
      )}
    </div>
  );
}

// CashCard is the cash position (FR-BNK-13): the server's total in the base currency, each account's balance and its
// value at the day's rate, and the accounts with no rate, which the total leaves out.
function CashCard({ position }: { position: CashPosition }) {
  const h = helpText("banking.cash-position");
  const noRate = position.no_rate ?? [];
  return (
    <section className="fy-card" aria-labelledby="cash-position">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="cash-position" title={h.tooltip}>
          {h.label}
        </h2>
        <span className="fy-small fy-muted">On {date(position.date)}, at the day's rates</span>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <p className="fy-tile__value fy-num">
          {formatAmount(position.total.amount)}
          <small>{position.total.currency}</small>
        </p>
        {position.accounts.length > 0 && (
          <table className="fy-table" aria-label="Cash by account">
            <thead>
              <tr>
                <th scope="col">Account</th>
                <th scope="col" className="fy-num">
                  Balance
                </th>
                <th scope="col" className="fy-num">
                  In {position.total.currency}
                </th>
              </tr>
            </thead>
            <tbody>
              {position.accounts.map((a) => (
                <tr key={a.money_account_id}>
                  <td>{a.name}</td>
                  <td className="fy-num">
                    {formatAmount(a.balance.amount)} {a.balance.currency}
                  </td>
                  <td className="fy-num">
                    {a.value ? formatAmount(a.value.amount) : a.balance.currency === position.total.currency ? formatAmount(a.balance.amount) : "No rate"}
                    {a.rate && <span className="fy-small fy-muted block">at {a.rate}</span>}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {noRate.length > 0 && (
          <p className="fy-small fy-row gap-2">
            <Icon of={Info} />
            No rate for {date(position.date)}: {noRate.join(", ")} {noRate.length === 1 ? "is" : "are"} left out of the total.
          </p>
        )}
      </div>
    </section>
  );
}

function AccountCard({ account: a, balance, may }: { account: MoneyAccount; balance?: components["schemas"]["CashAccount"]; may: boolean }) {
  const k = kinds[a.kind];
  const title = `account-${a.id}`;
  const complete = helpText("bank-account.complete-to");
  return (
    <article className="fy-card" aria-labelledby={title}>
      <div className="fy-card__head">
        <span className="fy-list__icon fy-list__icon--brand">
          <Icon of={k.icon} size={20} />
        </span>
        <div className="fy-grow fy-stack gap-0">
          <h3 className="fy-h3" id={title}>
            <Link to="/banking/accounts/$accountId" params={{ accountId: a.id }}>
              {a.name}
            </Link>
          </h3>
          <span className="fy-small fy-muted">
            {k.words}
            {a.institution && ` · ${a.institution}`} · {a.currency}
            {a.identifier && ` · ${a.identifier}`}
            <span className="fy-expert-only">
              {" "}
              · <span className="fy-mono">{a.account_code}</span>
            </span>
          </span>
        </div>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <div className="fy-row fy-row--between fy-row--top fy-row--wrap gap-3">
          {balance ? (
            <p className="fy-tile__value fy-num">
              {formatAmount(balance.balance.amount)}
              <small>{balance.balance.currency}</small>
            </p>
          ) : (
            <p className="fy-secondary">{a.closed_on ? `Closed on ${date(a.closed_on)}` : "No balance yet"}</p>
          )}
          <FreshnessChip account={a} />
        </div>
        <dl className="fy-dl">
          <div>
            <dt>Last statement</dt>
            <dd>{a.freshness.last_import ? dateTime(a.freshness.last_import) : "None yet"}</dd>
          </div>
          <div>
            <dt title={complete.tooltip}>{complete.label}</dt>
            <dd>{a.freshness.complete_to ? date(a.freshness.complete_to) : "Not yet"}</dd>
          </div>
          <div>
            <dt>Data source</dt>
            <dd>
              <SourceLine account={a} />
            </dd>
          </div>
          <div>
            <dt>Waiting</dt>
            <dd>{a.waiting === 0 ? "Nothing to categorise" : a.waiting === 1 ? "1 line to categorise" : `${a.waiting} lines to categorise`}</dd>
          </div>
        </dl>
        {a.freshness.open_gaps > 0 && (
          <p className="fy-error">
            <Icon of={TriangleAlert} />
            The balance chain breaks between two statements. Import the missing statement, or resolve the gap with the reason there is none.
          </p>
        )}
      </div>
      {may && !a.closed_on && (
        <div className="fy-card__foot">
          <Link className="fy-btn fy-btn--secondary fy-btn--sm" to="/banking/import" search={{ account: a.id }}>
            <Icon of={Upload} />
            Import
          </Link>
          <Link className="fy-btn fy-btn--ghost fy-btn--sm" to="/banking/accounts/$accountId" params={{ accountId: a.id }}>
            Open
          </Link>
        </div>
      )}
    </article>
  );
}
