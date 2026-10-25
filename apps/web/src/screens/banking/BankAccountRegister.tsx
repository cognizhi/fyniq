import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeftRight, BellRing, ChevronRight, CircleCheck, CircleSlash, Copy, Hourglass, PauseCircle, Scale, TriangleAlert, Upload } from "lucide-react";
import { useId, useState } from "react";
import { api, etagOf, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date, dateTime, timeZone } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { FreshnessChip, kinds, makers, SourceLine } from "./BankAccounts";
import { lastEnded, usePeriods } from "./BankReconciliation";

export type BankLine = components["schemas"]["BankLine"];
type Statement = components["schemas"]["BankStatementIn"];
type Gap = components["schemas"]["BankGap"];
type Signal = components["schemas"]["BankSignal"];

// LineStatus is a line's place in the queue (FR-BNK-06): reconciled, waiting, in suspense ⚠, a possible duplicate ⚠, or
// excluded. Each chip has an icon and a label.
export function LineStatus({ line }: { line: BankLine }) {
  switch (line.status) {
    case "categorised":
    case "matched":
    case "transferred":
      return (
        <span className="fy-chip fy-chip--good">
          <Icon of={CircleCheck} />
          Reconciled
        </span>
      );
    case "possible_duplicate":
      return (
        <span className="fy-chip fy-chip--warning">
          <Icon of={Copy} />
          Possible duplicate
        </span>
      );
    case "suspense":
      return (
        <span className="fy-chip fy-chip--warning">
          <Icon of={TriangleAlert} />
          In suspense
        </span>
      );
    case "excluded":
      return (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={CircleSlash} />
          Excluded
        </span>
      );
    default:
      return (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={PauseCircle} />
          Waiting
        </span>
      );
  }
}

const money = (m: components["schemas"]["Money"] | undefined) => (m ? `${formatAmount(m.amount)} ${m.currency}` : "Not given");

// BankAccountRegister is Banking › an account (docs/design/screens BankAccountRegister; FR-BNK-04, FR-BNK-09, FR-BNK-11,
// FR-BNK-16, FR-BNK-17; p1-bank-import-reconcile task 10.2): its statements and their continuity, gaps to resolve, its
// lines with their status, possible duplicates compared side by side, incoming-funds alerts as pending signals (never
// reconciled lines), and transfers in transit.
export function BankAccountRegister() {
  const { accountId } = useParams({ strict: false }) as { accountId: string };
  const { session } = useSignedIn();
  const may = (session.roles ?? []).some((r) => makers.includes(r));
  const queries = useQueryClient();
  const [compare, setCompare] = useState<BankLine | null>(null);
  const [resolving, setResolving] = useState<Gap | null>(null);
  const [dismissing, setDismissing] = useState<Signal | null>(null);
  const account = useQuery({
    queryKey: ["money-account", accountId],
    queryFn: async () => {
      const { data, error, response } = await api.GET("/v1/money-accounts/{money_account_id}", { params: { path: { money_account_id: accountId } } });
      if (error) throw error;
      return { ...data, etag: etagOf(response) };
    },
  });
  const statements = useQuery({
    queryKey: ["bank-statements", accountId],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/money-accounts/{money_account_id}/statements", { params: { path: { money_account_id: accountId }, query: { page_size: 50 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const lines = useInfiniteQuery({
    queryKey: ["bank-lines", accountId],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/v1/bank-lines", { params: { query: { money_account_id: accountId, page_size: 100, cursor: pageParam } } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const signals = useQuery({
    queryKey: ["bank-signals", "pending"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/bank-signals", { params: { query: { status: "pending", page_size: 100 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const transfers = useQuery({
    queryKey: ["bank-transfers"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/bank-transfers", { params: { query: { page_size: 100 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const periods = usePeriods();
  const month = lastEnded(periods.data ?? [], new Date().toLocaleDateString("en-CA", { timeZone }));
  const refresh = () => {
    for (const key of [["money-account", accountId], ["bank-lines", accountId], ["bank-signals"], ["money-accounts"]]) void queries.invalidateQueries({ queryKey: key });
  };
  const a = account.data;
  const items = lines.data?.pages.flatMap((p) => p.items ?? []) ?? [];
  const mine = (signals.data ?? []).filter((s) => s.money_account_id === accountId);
  const moving = (transfers.data ?? []).filter((t) => t.from_money_account_id === accountId || t.to_money_account_id === accountId);
  const openGaps = (a?.gaps ?? []).filter((g) => !g.resolution);
  const duplicates = items.filter((l) => l.status === "possible_duplicate").length;

  if (account.isError) return <Refusal problem={problemOf(account.error)} title="The money account could not be read" />;
  if (!a) return <p className="fy-secondary">Loading the money account…</p>;
  return (
    <div className="fy-page fy-enter">
      <nav className="fy-breadcrumb" aria-label="Breadcrumb">
        <Link to="/banking">Banking</Link>
        <Icon of={ChevronRight} />
        <span>{a.name}</span>
      </nav>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">{a.name}</h1>
          <p className="fy-secondary">
            {kinds[a.kind].words}
            {a.institution && ` · ${a.institution}`} · {a.currency}
            {a.identifier && ` · ${a.identifier}`}
            <span className="fy-expert-only">
              {" "}
              · <span className="fy-mono">{a.account_code}</span>
            </span>
          </p>
        </div>
        <div className="fy-pagehead__actions">
          {month && (
            <Link className="fy-btn fy-btn--ghost fy-expert-only" to="/banking/reconciliations/$accountId/$periodId" params={{ accountId: a.id, periodId: month.id }}>
              <Icon of={Scale} />
              Reconcile
            </Link>
          )}
          {may && (
            <Link className="fy-btn fy-btn--primary" to="/banking/import" search={{ account: a.id }}>
              <Icon of={Upload} />
              Import statement
            </Link>
          )}
        </div>
      </div>
      <ReviewBanner screen="bank-account" />
      <div className="fy-row fy-row--wrap gap-3">
        <FreshnessChip account={a} />
        <span className="fy-small fy-secondary">
          <SourceLine account={a} />
        </span>
        <span className="fy-small fy-muted">Last statement {a.freshness.last_import ? dateTime(a.freshness.last_import) : "none yet"}</span>
        {duplicates > 0 && (
          <span className="fy-chip fy-chip--warning">
            <Icon of={Copy} />
            {duplicates === 1 ? "1 possible duplicate" : `${duplicates} possible duplicates`}
          </span>
        )}
      </div>

      {openGaps.length > 0 && (
        <section className="fy-banner fy-banner--warning" aria-labelledby="gaps">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong" id="gaps">
              {openGaps.length === 1 ? "A gap in its statements" : `${openGaps.length} gaps in its statements`}
            </span>
            {openGaps.map((g) => (
              <span key={g.id} className="fy-small fy-row fy-row--wrap gap-2">
                {g.after_date ? `After ${date(g.after_date)}` : "Before the first statement"}, before {date(g.before_date)}: expected an opening of {money(g.expected_opening)}, found{" "}
                {money(g.found_opening)}.
                {may && (
                  <button type="button" className="fy-btn fy-btn--secondary fy-btn--sm" onClick={() => setResolving(g)}>
                    Resolve
                  </button>
                )}
              </span>
            ))}
          </div>
        </section>
      )}

      {mine.length > 0 && (
        <section className="fy-card" aria-labelledby="alerts">
          <div className="fy-card__head">
            <h2 className="fy-h3 fy-row gap-2" id="alerts">
              <Icon of={BellRing} />
              Incoming-funds alerts
            </h2>
            <span className="fy-small fy-muted">Pending: not yet in a statement, nothing posted</span>
          </div>
          <ul className="fy-card__body fy-stack fy-stack--2">
            {mine.map((s) => (
              <li key={s.id} className="fy-row fy-row--between fy-row--wrap gap-2">
                <span>
                  <span className="fy-num fy-strong">{money(s.amount)}</span> from {s.payer ?? "an unnamed payer"} · {dateTime(s.received_at)}
                  {s.invoice && (
                    <>
                      {" "}
                      ·{" "}
                      <Link to="/sales/invoices/$invoiceId" params={{ invoiceId: s.invoice.id }}>
                        {s.invoice.number ?? "the invoice"} likely paid
                      </Link>
                    </>
                  )}
                </span>
                {may && (
                  <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" onClick={() => setDismissing(s)}>
                    Dismiss
                  </button>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}

      {moving.length > 0 && (
        <section className="fy-card" aria-labelledby="transit">
          <div className="fy-card__head">
            <h2 className="fy-h3 fy-row gap-2" id="transit">
              <Icon of={ArrowLeftRight} />
              Transfers in transit
            </h2>
          </div>
          <ul className="fy-card__body fy-stack fy-stack--2">
            {moving.map((t) => (
              <li key={t.id} className="fy-row fy-row--wrap gap-2">
                <Icon of={Hourglass} />
                {date(t.date)}: {t.from_money_account_id === accountId ? `${money(t.paid)} out` : `${money(t.received)} in`}, its other side's line not yet arrived
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="fy-stack fy-stack--3" aria-labelledby="statements">
        <h2 className="fy-h2" id="statements">
          Statements
        </h2>
        {statements.isError && <Refusal problem={problemOf(statements.error)} title="The statements could not be listed" />}
        {statements.data && statements.data.length === 0 && <p className="fy-secondary">No statements yet. Import the bank's first statement, or wait for its first report.</p>}
        {statements.data && statements.data.length > 0 && <Continuity statements={statements.data} gaps={a.gaps ?? []} />}
      </section>

      <section className="fy-stack fy-stack--3" aria-labelledby="lines">
        <h2 className="fy-h2" id="lines">
          Lines
        </h2>
        {lines.isError && <Refusal problem={problemOf(lines.error)} title="The lines could not be listed" />}
        {lines.data && items.length === 0 && <p className="fy-secondary">No lines yet.</p>}
        {items.length > 0 && (
          <div className="fy-tablewrap">
            <table className="fy-table fy-table--cards" aria-label="Statement lines">
              <thead>
                <tr>
                  <th scope="col">Booked</th>
                  <th scope="col">Description</th>
                  <th scope="col">Counterparty</th>
                  <th scope="col" className="fy-num">
                    Amount
                  </th>
                  <th scope="col" className="fy-num fy-expert-only">
                    Balance
                  </th>
                  <th scope="col">Status</th>
                </tr>
              </thead>
              <tbody>
                {items.map((l) => (
                  <tr key={l.id}>
                    <td data-card="meta">
                      {date(l.booking_date)}
                      {l.value_date && <span className="fy-small fy-muted block">Value {date(l.value_date)}</span>}
                    </td>
                    <td data-card="title">
                      <span className="fy-mono">{l.description ?? ""}</span>
                      {l.reference && <span className="fy-small fy-muted block fy-mono">{l.reference}</span>}
                    </td>
                    <td data-card="meta">{l.counterparty ?? ""}</td>
                    <td data-card="amount" className="fy-num">
                      {formatAmount(l.amount.amount)}
                    </td>
                    <td className="fy-num fy-expert-only">{l.balance ? formatAmount(l.balance.amount) : ""}</td>
                    <td data-card="status">
                      <span className="fy-row fy-row--wrap gap-2">
                        <LineStatus line={l} />
                        {l.status === "possible_duplicate" && may && (
                          <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" onClick={() => setCompare(l)}>
                            Compare
                          </button>
                        )}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {lines.hasNextPage && (
          <div>
            <button type="button" className="fy-btn fy-btn--secondary" onClick={() => void lines.fetchNextPage()} disabled={lines.isFetchingNextPage}>
              Show more
            </button>
          </div>
        )}
        <p className="fy-small fy-muted">Descriptions are as the bank sent them, shown as text and never followed.</p>
      </section>

      {compare && (
        <CompareDialog
          line={compare}
          onClose={() => setCompare(null)}
          onDone={() => {
            setCompare(null);
            refresh();
          }}
        />
      )}
      {resolving && (
        <ResolveGapDialog
          accountId={a.id}
          gap={resolving}
          onClose={() => setResolving(null)}
          onDone={() => {
            setResolving(null);
            refresh();
          }}
        />
      )}
      {dismissing && (
        <DismissDialog
          signal={dismissing}
          onClose={() => setDismissing(null)}
          onDone={() => {
            setDismissing(null);
            refresh();
          }}
        />
      )}
    </div>
  );
}

// Continuity is each statement in its chain (FR-BNK-16): its days, opening and closing balances, lines and how it came,
// with a gap ⚠ where its opening is not the previous closing.
function Continuity({ statements, gaps }: { statements: Statement[]; gaps: Gap[] }) {
  return (
    <div className="fy-tablewrap">
      <table className="fy-table fy-table--cards" aria-label="Statements and their continuity">
        <thead>
          <tr>
            <th scope="col">Days</th>
            <th scope="col" className="fy-num">
              Opening
            </th>
            <th scope="col" className="fy-num">
              Closing
            </th>
            <th scope="col" className="fy-num">
              Lines
            </th>
            <th scope="col">Came as</th>
            <th scope="col">Continuity</th>
          </tr>
        </thead>
        <tbody>
          {statements.map((s) => {
            const gap = gaps.find((g) => g.before_date === s.from && !g.resolution);
            return (
              <tr key={s.id}>
                <td data-card="title">
                  {date(s.from)} to {date(s.to)}
                </td>
                <td data-card="meta" className="fy-num">
                  {s.opening ? formatAmount(s.opening.amount) : "Not given"}
                </td>
                <td data-card="meta" className="fy-num">
                  {s.closing ? formatAmount(s.closing.amount) : "Not given"}
                </td>
                <td data-card="meta" className="fy-num">
                  {s.lines}
                </td>
                <td data-card="meta">
                  {s.source === "email" ? "Emailed report" : s.source === "connector" ? "Connector" : "File"} · <span className="fy-mono">{s.format}</span>
                </td>
                <td data-card="status">
                  {gap ? (
                    <span className="fy-chip fy-chip--warning">
                      <Icon of={TriangleAlert} />
                      Gap before it
                    </span>
                  ) : (
                    <span className="fy-chip fy-chip--good">
                      <Icon of={CircleCheck} />
                      Continues
                    </span>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function useLine(id: string | undefined) {
  return useQuery({
    queryKey: ["bank-line", id],
    enabled: Boolean(id),
    queryFn: async () => {
      const { data, error, response } = await api.GET("/v1/bank-lines/{line_id}", { params: { path: { line_id: id! } } });
      if (error) throw error;
      return { ...data, etag: etagOf(response) };
    },
  });
}

// CompareDialog shows a possible duplicate beside the line it may duplicate (FR-BNK-04): keep it as a line of its own, or
// exclude it with a reason. Neither posts anything.
function CompareDialog({ line, onClose, onDone }: { line: BankLine; onClose: () => void; onDone: () => void }) {
  const id = useId();
  const mine = useLine(line.id);
  const other = useLine(line.duplicate_of);
  const [reason, setReason] = useState("");
  const [missing, setMissing] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const act = async (what: "keep" | "exclude") => {
    if (what === "exclude" && !reason.trim()) return setMissing(true);
    if (!mine.data) return;
    setBusy(true);
    setProblem(null);
    const header = { "Idempotency-Key": newKey(), "If-Match": mine.data.etag };
    const out =
      what === "keep"
        ? await api.POST("/v1/bank-lines/{line_id}/keep", { params: { path: { line_id: line.id }, header } })
        : await api.POST("/v1/bank-lines/{line_id}/exclude", { params: { path: { line_id: line.id }, header }, body: { reason: reason.trim() } });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone();
  };
  const side = (title: string, l: BankLine | undefined) => (
    <div className="fy-card fy-grow">
      <div className="fy-card__head">
        <h3 className="fy-h3">{title}</h3>
      </div>
      {l ? (
        <dl className="fy-dl fy-card__body">
          <div>
            <dt>Booked</dt>
            <dd>{date(l.booking_date)}</dd>
          </div>
          <div>
            <dt>Amount</dt>
            <dd className="fy-num">{money(l.amount)}</dd>
          </div>
          <div>
            <dt>Description</dt>
            <dd className="fy-mono">{l.description ?? ""}</dd>
          </div>
          <div>
            <dt>Reference</dt>
            <dd className="fy-mono">{l.reference ?? "None"}</dd>
          </div>
          <div>
            <dt>Balance after it</dt>
            <dd className="fy-num">{l.balance ? money(l.balance) : "Not given"}</dd>
          </div>
          <div>
            <dt>Status</dt>
            <dd>
              <LineStatus line={l} />
            </dd>
          </div>
        </dl>
      ) : (
        <p className="fy-card__body fy-secondary">Loading…</p>
      )}
    </div>
  );
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title="Is it a duplicate?"
      description="The same money twice, or two real payments that look alike? Keeping it makes it a line of its own; excluding it takes it out of the queue. Neither posts anything."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="secondary" onClick={() => void act("keep")} disabled={busy}>
            Keep it
          </Button>
          <Button variant="primary" onClick={() => void act("exclude")} disabled={busy}>
            Exclude it
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The line was not changed" />}
      <div className="fy-row fy-row--wrap fy-row--top gap-3">
        {side("This line", mine.data)}
        {side("The line it may duplicate", other.data)}
      </div>
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-reason`}>
          Why exclude it
        </label>
        <input
          className="fy-input"
          id={`${id}-reason`}
          value={reason}
          maxLength={500}
          placeholder="Such as: the same line in two statements"
          onChange={(e) => setReason(e.target.value)}
          aria-invalid={missing || undefined}
          aria-describedby={missing ? `${id}-reason-error` : undefined}
        />
        {missing && (
          <span className="fy-error" id={`${id}-reason-error`}>
            Say why it is excluded: the reason is kept in the audit trail.
          </span>
        )}
      </div>
    </Dialog>
  );
}

// ResolveGapDialog resolves a gap with its reason (FR-BNK-16): the missing statement imported, or why there is none.
function ResolveGapDialog({ accountId, gap, onClose, onDone }: { accountId: string; gap: Gap; onClose: () => void; onDone: () => void }) {
  const id = useId();
  const [resolution, setResolution] = useState("");
  const [missing, setMissing] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const send = async () => {
    if (!resolution.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/money-accounts/{money_account_id}/gaps/{gap_id}/resolve", {
      params: { path: { money_account_id: accountId, gap_id: gap.id }, header: { "Idempotency-Key": newKey() } },
      body: { resolution: resolution.trim() },
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
      title="Resolve the gap"
      description={`Before ${date(gap.before_date)} the balance chain breaks. Import the missing statement, which closes the gap by itself, or say why there is none.`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void send()} disabled={busy}>
            Resolve
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The gap was not resolved" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-resolution`}>
          How it was resolved
        </label>
        <input
          className="fy-input"
          id={`${id}-resolution`}
          value={resolution}
          maxLength={500}
          placeholder="Such as: no transactions on those days; confirmed with the bank"
          onChange={(e) => setResolution(e.target.value)}
          aria-invalid={missing || undefined}
          aria-describedby={missing ? `${id}-resolution-error` : undefined}
        />
        {missing && (
          <span className="fy-error" id={`${id}-resolution-error`}>
            Say how it was resolved: it is kept in the audit trail.
          </span>
        )}
      </div>
    </Dialog>
  );
}

// DismissDialog dismisses a pending incoming-funds signal with its reason (FR-BNK-16): its invoice stops showing
// "likely paid"; nothing is posted.
export function DismissDialog({ signal, onClose, onDone }: { signal: Signal; onClose: () => void; onDone: () => void }) {
  const id = useId();
  const h = helpText("bank-signal.dismiss");
  const [reason, setReason] = useState("");
  const [missing, setMissing] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const send = async () => {
    if (!reason.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/bank-signals/{signal_id}/dismiss", {
      params: { path: { signal_id: signal.id }, header: { "Idempotency-Key": newKey() } },
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
      title={h.label}
      description={h.tooltip}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void send()} disabled={busy}>
            {h.label}
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The alert was not dismissed" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-reason`}>
          Why dismiss it
        </label>
        <input
          className="fy-input"
          id={`${id}-reason`}
          value={reason}
          maxLength={500}
          placeholder="Such as: it pays another invoice"
          onChange={(e) => setReason(e.target.value)}
          aria-invalid={missing || undefined}
          aria-describedby={missing ? `${id}-reason-error` : undefined}
        />
        {missing && (
          <span className="fy-error" id={`${id}-reason-error`}>
            Say why it is dismissed: the reason is kept in the audit trail.
          </span>
        )}
      </div>
    </Dialog>
  );
}
