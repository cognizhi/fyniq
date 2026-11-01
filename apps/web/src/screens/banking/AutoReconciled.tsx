import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowLeftRight, Bot, Hash, Undo2, Zap } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { BankTabs, makers } from "./BankAccounts";

type Auto = components["schemas"]["AutoReconciliation"];

// basisWords says what made a system reconciliation (FR-BNK-14): an exact match, a payment of the batch the line named
// (rev 0.160), a transfer's other side, or a rule.
export function basisWords(a: Auto): { words: string; icon: typeof Hash } {
  if (a.batch_number) return { words: `A payment of ${a.batch_number}, which the line names`, icon: Hash };
  switch (a.line.match?.basis) {
    case "transfer":
      return { words: "The other side of a transfer", icon: ArrowLeftRight };
    case "rule":
      return { words: `The rule ${a.rule_name ?? ""}`.trim(), icon: Zap };
    default:
      return { words: "Exact match: amount, number and contact", icon: Hash };
  }
}

// AutoReconciled is Banking › Auto-reconciled (docs/design/screens AutoReconciled; FR-BNK-14, FR-AUT-02;
// p1-bank-import-reconcile task 10.4): what the system reconciled without a click and still stands, why, by which
// principal and when, each undone with a reason while its period is open; and the limits it worked within.
export function AutoReconciled() {
  const { session } = useSignedIn();
  const may = (session.roles ?? []).some((r) => makers.includes(r));
  const queries = useQueryClient();
  const [undoing, setUndoing] = useState<Auto | null>(null);
  const list = useInfiniteQuery({
    queryKey: ["auto-reconciliations"],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/v1/auto-reconciliations", { params: { query: { page_size: 100, cursor: pageParam } } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const limits = useQuery({
    queryKey: ["settings", "auto-reconcile"],
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/organisation/settings", { params: { query: { page_size: 200 } } });
      if (error) throw error;
      return (data.items ?? []).filter((s) => s.key === "bank.auto_cap_line" || s.key === "bank.auto_cap_rule_day");
    },
  });
  const items = list.data?.pages.flatMap((p) => p.items ?? []) ?? [];
  const h = helpText("banking.auto-reconciled");

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Banking</span>
          <h1 className="fy-h1">Auto-reconciled</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
      </div>
      <ReviewBanner screen="auto-reconciled" />
      <BankTabs />
      {limits.data && limits.data.length > 0 && (
        <p className="fy-small fy-secondary">
          Within the limits:{" "}
          {limits.data.map((s, n) => (
            <span key={s.key}>
              {n > 0 && "; "}
              {s.label} <span className="fy-num">S${formatAmount(s.value)}</span>
            </span>
          ))}
          . Each week the 5 largest and 5 others at random join the weekly review.
        </p>
      )}
      {list.isError && <Refusal problem={problemOf(list.error)} title="What the system reconciled could not be listed" />}
      {list.data && items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={Bot} size={24} />
          <h2 className="fy-h3">Nothing reconciled by itself in the last 30 days</h2>
          <p className="fy-secondary">Exact matches, a transfer's other side and lines an approved rule meets show here once Fyniq reconciles them.</p>
        </div>
      )}
      {items.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label="Lines reconciled by the system">
            <thead>
              <tr>
                <th scope="col">Booked</th>
                <th scope="col">Line</th>
                <th scope="col" className="fy-num">
                  Amount
                </th>
                <th scope="col">Why</th>
                <th scope="col">By</th>
                <th scope="col">Undo</th>
              </tr>
            </thead>
            <tbody>
              {items.map((a) => {
                const b = basisWords(a);
                return (
                  <tr key={a.line.id}>
                    <td data-card="meta">{date(a.line.booking_date)}</td>
                    <td data-card="title">
                      <Link to="/banking/accounts/$accountId" params={{ accountId: a.line.money_account_id }} className="fy-mono">
                        {a.line.description ?? "A line"}
                      </Link>
                    </td>
                    <td data-card="amount" className="fy-num">
                      {formatAmount(a.line.amount.amount)} {a.line.amount.currency}
                    </td>
                    <td data-card="meta">
                      <span className="fy-row gap-2">
                        <Icon of={b.icon} />
                        {b.words}
                      </span>
                    </td>
                    <td data-card="meta">
                      <span className="fy-mono fy-small">{a.line.match?.made_by ?? ""}</span>
                      {a.line.match?.made_at && <span className="fy-small fy-muted block">{dateTime(a.line.match.made_at)}</span>}
                    </td>
                    <td>
                      {may && (
                        <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" onClick={() => setUndoing(a)} aria-label={`Undo the line of ${date(a.line.booking_date)}`}>
                          <Icon of={Undo2} />
                          Undo
                        </button>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {list.hasNextPage && (
        <div>
          <button type="button" className="fy-btn fy-btn--secondary" onClick={() => void list.fetchNextPage()} disabled={list.isFetchingNextPage}>
            Show more
          </button>
        </div>
      )}
      {undoing && (
        <UndoDialog
          auto={undoing}
          onClose={() => setUndoing(null)}
          onDone={() => {
            setUndoing(null);
            for (const key of [["auto-reconciliations"], ["bank-lines"], ["money-accounts"]]) void queries.invalidateQueries({ queryKey: key });
          }}
        />
      )}
    </div>
  );
}

// UndoDialog puts a line the system reconciled back in the queue with a reason, while its period is open: what it posted
// is reversed or voided.
function UndoDialog({ auto, onClose, onDone }: { auto: Auto; onClose: () => void; onDone: () => void }) {
  const id = useId();
  const [reason, setReason] = useState("");
  const [missing, setMissing] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const go = async () => {
    if (!reason.trim()) return setMissing(true);
    setBusy(true);
    setProblem(null);
    const out = await api.POST("/v1/bank-lines/{line_id}/unreconcile", {
      params: { path: { line_id: auto.line.id }, header: { "Idempotency-Key": newKey(), "If-Match": `"${auto.line.version}"` } },
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
      title="Undo it"
      description="The line goes back in the queue, and what it posted is reversed or voided on its own date. Its month must still be open."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => void go()} disabled={busy}>
            Undo it
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="It was not undone" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-reason`}>
          Why undo it
        </label>
        <input
          className="fy-input"
          id={`${id}-reason`}
          value={reason}
          maxLength={500}
          placeholder="Such as: matched to the wrong invoice"
          onChange={(e) => setReason(e.target.value)}
          aria-invalid={missing || undefined}
          aria-describedby={missing ? `${id}-reason-error` : undefined}
        />
        {missing && (
          <span className="fy-error" id={`${id}-reason-error`}>
            Say why: the reason is kept with the reversal.
          </span>
        )}
      </div>
    </Dialog>
  );
}
