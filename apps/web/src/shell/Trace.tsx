import { Dialog as Primitive } from "@base-ui/react/dialog";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRight, GitCommitHorizontal, Maximize2, X } from "lucide-react";
import { useContext, useId, useState, type ReactNode } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { Refusal } from "../components/Refusal";
import { buttonVariants } from "../components/ui/button";
import { DeviceRoot } from "../components/ui/dialog";
import { formatAmount } from "../format/amount";
import { date, dateTime } from "../format/date";
import { helpText } from "../i18n";
import { Icon, problemOf } from "../ui";

type AccountLedger = components["schemas"]["AccountLedger"];
type LedgerLine = components["schemas"]["LedgerLine"];
type Entry = components["schemas"]["LedgerEntryView"];
type AuditEvent = components["schemas"]["AuditEventSummary"];

// TraceAccount is an account a figure is made of, with its share of the figure as the report shows it.
export type TraceAccount = { id: string; code: string; name: string; amount?: string };

// TraceFigure is a figure the Trace panel follows (FR-UX-04): what it is, where it was read and the accounts behind it.
export type TraceFigure = {
  title: string; // the line or total, such as "Software subscriptions"
  amount: string;
  currency: string;
  context: string; // the report it was read in, such as "Profit and loss, September 2026"
  from: string;
  to: string;
  revision: number; // the ledger revision the report read (FR-RPT-08)
  accounts: readonly TraceAccount[];
  values?: readonly string[]; // the dimension values the figure is of (FR-DIM-02)
};

type Level = "breakdown" | "lines" | "source" | "history";

const levelNames: Record<Level, string> = { breakdown: "Breakdown", lines: "Ledger lines", source: "Source", history: "History" };

// linkButton is a button that reads as a link: the trace path's steps.
const linkButton = "cursor-pointer border-0 bg-transparent p-0 text-start font-[inherit] text-[var(--link)] underline-offset-2 hover:underline";

// Dr and Cr in Expert mode, the signed amount in Basic: the ledger's own sign, never recomputed (RND-DISPLAY).
function Amount({ value }: { value: string }) {
  const credit = value.startsWith("-");
  return (
    <span className="fy-num">
      <span className="fy-small fy-muted fy-expert-only">{credit ? "Cr " : "Dr "}</span>
      <span className="fy-expert-only">{formatAmount(credit ? value.slice(1) : value)}</span>
      <span className="fy-basic-only">{formatAmount(value)}</span>
    </span>
  );
}

// words is an audit action in plain words: its last part, such as "posted" for document.posted.
export function words(action: string): string {
  const last = action.split(".").pop() ?? action;
  const w = last.replaceAll("_", " ");
  return w.charAt(0).toUpperCase() + w.slice(1);
}

const actorWords: Record<AuditEvent["actor"]["type"], string> = {
  user: "a person",
  service: "a service",
  integration: "an integration",
  scheduler: "the schedule",
  ai: "the AI, for a person",
  system: "Fyniq",
};

// lineTitle is what a ledger line is, in words: its memo or description, and its document's number.
function lineTitle(l: LedgerLine): ReactNode {
  const what = l.memo || l.description || (l.source_type === "opening_balance" ? "Opening balance" : "Entry");
  return (
    <>
      {what}
      {l.number && (
        <>
          {" · "}
          <span className="fy-mono">{l.number}</span>
        </>
      )}
      {l.reversal && " (reversal)"}
    </>
  );
}

function Lines({ figure, account, onLine }: { figure: TraceFigure; account: TraceAccount; onLine: (l: LedgerLine) => void }) {
  const ledger = useQuery({
    queryKey: ["trace", "ledger", account.id, figure.from, figure.to, figure.revision, figure.values],
    queryFn: async (): Promise<AccountLedger | undefined> => {
      const r = await api.GET("/v1/reports/general-ledger", {
        params: {
          query: {
            account_id: [account.id],
            from: figure.from,
            to: figure.to,
            as_at_revision: figure.revision,
            ...(figure.values?.length ? { dimension_value_id: [...figure.values] } : {}),
          },
        },
      });
      if (r.error) throw r.error;
      return r.data.accounts[0];
    },
  });
  if (ledger.isError) return <Refusal problem={problemOf(ledger.error)} title="The ledger lines could not be read" />;
  if (ledger.isPending) return <p className="fy-secondary p-4">Reading the ledger lines…</p>;
  const a = ledger.data;
  if (!a) return null;
  return (
    <ul className="fy-list" aria-label={`Ledger lines of ${account.name}`}>
      {a.opening.amount !== "0.00" && (
        <li>
          <div className="fy-list__item fy-muted">
            <span className="fy-grow">Balance on {date(figure.from)}</span>
            <Amount value={a.opening.amount} />
          </div>
        </li>
      )}
      {a.lines.length === 0 && (
        <li>
          <p className="fy-list__item fy-secondary">No lines from {date(figure.from)} to {date(figure.to)}.</p>
        </li>
      )}
      {a.lines.map((l) => (
        <li key={`${l.entry_id}-${l.ledger_revision}-${l.posting_date}-${l.base.amount}-${l.description ?? ""}`}>
          <button type="button" className="fy-list__item w-full text-left" onClick={() => onLine(l)}>
            <span className="fy-list__main">
              <span className="fy-list__title">{lineTitle(l)}</span>
              <span className="fy-list__meta">
                {date(l.posting_date)}
                <span className="fy-expert-only">
                  {" · "}
                  <span className="fy-mono">{account.code}</span>
                  {` · revision ${l.ledger_revision}`}
                </span>
              </span>
            </span>
            <Amount value={l.base.amount} />
          </button>
        </li>
      ))}
      <li>
        <div className="fy-list__item fy-strong">
          <span className="fy-grow">
            {a.lines.length} {a.lines.length === 1 ? "line" : "lines"}
            <span className="fy-expert-only fy-small fy-muted">
              {" · debits "}
              {formatAmount(a.debits.amount)}
              {" · credits "}
              {formatAmount(a.credits.amount)}
            </span>
          </span>
          <span className="fy-small fy-muted">Balance on {date(figure.to)}</span>
          <Amount value={a.closing.amount} />
        </div>
      </li>
    </ul>
  );
}

function Source({ entryId, onHistory }: { entryId: string; onHistory: () => void }) {
  const entry = useQuery({
    queryKey: ["trace", "entry", entryId],
    queryFn: async (): Promise<Entry> => {
      const r = await api.GET("/v1/ledger/entries/{entry_id}", { params: { path: { entry_id: entryId } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const id = useId();
  if (entry.isError) return <Refusal problem={problemOf(entry.error)} title="The entry could not be read" />;
  if (entry.isPending) return <p className="fy-secondary p-4">Reading the entry…</p>;
  const e = entry.data;
  const s = e.source;
  const name = s.number || s.document_type?.replaceAll("_", " ") || s.type.replaceAll("_", " ");
  return (
    <section className="fy-stack fy-stack--3 p-4" aria-labelledby={id}>
      <div className="fy-row">
        <h3 className="fy-h3 fy-grow" id={id}>
          Source: <span className="fy-mono">{name}</span>
        </h3>
        {s.link && (
          <Link className={buttonVariants({ variant: "ghost", size: "sm" })} to={s.link}>
            Open it
          </Link>
        )}
      </div>
      <dl className="fy-stack fy-stack--2">
        {s.party && (
          <div className="fy-row fy-row--between fy-small">
            <dt className="fy-muted">Customer or supplier</dt>
            <dd>{s.party}</dd>
          </div>
        )}
        {s.date && (
          <div className="fy-row fy-row--between fy-small">
            <dt className="fy-muted">Its date</dt>
            <dd>{date(s.date)}</dd>
          </div>
        )}
        <div className="fy-row fy-row--between fy-small">
          <dt className="fy-muted">Posted on</dt>
          <dd>{date(e.posting_date)}</dd>
        </div>
        {s.status && (
          <div className="fy-row fy-row--between fy-small">
            <dt className="fy-muted">Status now</dt>
            <dd>{s.status.replaceAll("_", " ")}</dd>
          </div>
        )}
        {e.memo && (
          <div className="fy-row fy-row--between fy-small">
            <dt className="fy-muted">Memo</dt>
            <dd>{e.memo}</dd>
          </div>
        )}
        {s.current_version !== undefined && s.current_version > s.version && (
          <p className="fy-small fy-secondary">It was changed since: this entry is version {s.version}&apos;s, and it is at version {s.current_version} now.</p>
        )}
        {e.reverses && <p className="fy-small fy-secondary">This entry reverses an earlier one.</p>}
      </dl>
      <table className="fy-table fy-expert-only">
        <caption className="fy-sr">The entry&apos;s lines</caption>
        <thead>
          <tr>
            <th scope="col">Account</th>
            <th scope="col" className="fy-num">
              Amount ({e.lines[0]?.base.currency ?? ""})
            </th>
          </tr>
        </thead>
        <tbody>
          {e.lines.map((l) => (
            <tr key={l.line_no}>
              <td>
                <span className="fy-mono fy-muted">{l.code}</span> {l.name}
                {l.description && <span className="fy-small fy-muted"> · {l.description}</span>}
              </td>
              <td className="fy-num">
                <Amount value={l.base.amount} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="fy-small fy-muted">Revision {e.ledger_revision}</p>
      {s.document_type && (
        <button type="button" className={buttonVariants({ variant: "secondary", size: "sm" })} onClick={onHistory}>
          See its history
        </button>
      )}
    </section>
  );
}

function History({ documentId }: { documentId: string }) {
  const events = useQuery({
    queryKey: ["trace", "history", documentId],
    queryFn: async () => {
      const r = await api.GET("/v1/audit/events", { params: { query: { record_type: "document", record_id: documentId, page_size: 50 } } });
      if (r.error) throw r.error;
      return r.data.items;
    },
  });
  const id = useId();
  if (events.isError) {
    const p = problemOf(events.error);
    if (p.code?.startsWith("API-AUTHZ"))
      return <p className="fy-secondary p-4">Its history is the audit trail&apos;s, which your roles do not show. Someone who reads the audit trail can follow it.</p>;
    return <Refusal problem={p} title="The history could not be read" />;
  }
  if (events.isPending) return <p className="fy-secondary p-4">Reading the history…</p>;
  return (
    <section className="fy-stack fy-stack--3 p-4" aria-labelledby={id}>
      <h3 className="fy-h3" id={id}>
        History
      </h3>
      {events.data.length === 0 ? (
        <p className="fy-secondary">The audit trail holds nothing about it.</p>
      ) : (
        <ol className="fy-timeline pl-7">
          {events.data.map((x) => (
            <li key={x.id} className="fy-timeline__item">
              <span className="fy-timeline__dot" aria-hidden="true" />
              <span className="fy-small fy-strong">{words(x.action)}</span>
              <span className="fy-small fy-muted">
                {dateTime(x.occurred_at)} · by {actorWords[x.actor.type] ?? x.actor.type}
                {x.reason ? ` · ${x.reason}` : ""}
              </span>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

// TracePanel is the Trace panel (docs/design/screens TracePanel; FR-UX-04, FR-UX-05, FR-RPT-08): a figure's breakdown
// into its accounts, an account's ledger lines read as at the report's revision, a line's entry and source document,
// and that document's history from the audit trail for those who may read it. It opens over the report as a side peek
// and keeps its place; closing it, or Back, returns to the report as it was.
export function TracePanel({ figure, onClose }: { figure: TraceFigure; onClose: () => void }) {
  const device = useContext(DeviceRoot);
  const id = useId();
  const single = figure.accounts.length === 1 ? figure.accounts[0] : undefined;
  const [level, setLevel] = useState<Level>(single ? "lines" : "breakdown");
  const [account, setAccount] = useState<TraceAccount | undefined>(single);
  const [line, setLine] = useState<LedgerLine | undefined>();
  const [documentId, setDocumentId] = useState<string | undefined>();
  const h = helpText("statement.trace");
  const open = (to: Level) => setLevel(to);
  const reachable: Record<Level, boolean> = {
    breakdown: figure.accounts.length > 1,
    lines: account !== undefined,
    source: line !== undefined,
    history: documentId !== undefined,
  };
  const crumbs: { level: Level; label: ReactNode }[] = [
    ...(figure.accounts.length > 1 ? [{ level: "breakdown" as Level, label: figure.title }] : []),
    ...(account ? [{ level: "lines" as Level, label: account.name }] : []),
    ...(line ? [{ level: "source" as Level, label: line.number ?? "The entry" }] : []),
    ...(level === "history" ? [{ level: "history" as Level, label: "History" }] : []),
  ];
  return (
    <Primitive.Root open onOpenChange={(o) => (o ? undefined : onClose())}>
      <Primitive.Portal container={device ?? undefined}>
        <Primitive.Backdrop className="fixed inset-0 z-(--z-modal) bg-scrim" />
        <Primitive.Popup className="fy-sheet fixed inset-y-0 right-0 z-(--z-modal)" aria-labelledby={`${id}-title`}>
          <div className="fy-row fy-row--top px-4 pt-4 pb-2">
            <div className="fy-stack fy-stack--2 fy-grow">
              <span className="fy-eyebrow">{h.label}</span>
              <Primitive.Title className="fy-h2" id={`${id}-title`}>
                {figure.title}
              </Primitive.Title>
              <span className="fy-row fy-row--wrap gap-2">
                <span className="fy-tile__value fy-num">{formatAmount(figure.amount)}</span>
                <span className="fy-small fy-muted">
                  {figure.currency} · {date(figure.from)} to {date(figure.to)}
                </span>
              </span>
            </div>
            {account && (
              <Link
                className={buttonVariants({ variant: "ghost", size: "icon" })}
                to="/reports/general-ledger"
                search={{ account_id: [account.id], from: figure.from, to: figure.to, ...(figure.values?.length ? { dimension_value_id: [...figure.values] } : {}) }}
                aria-label="Open the ledger lines as a full page"
                title="Open the ledger lines as a full page"
              >
                <Icon of={Maximize2} size={20} />
              </Link>
            )}
            <Primitive.Close className={buttonVariants({ variant: "ghost", size: "icon" })} aria-label="Close the trace and return to the report">
              <Icon of={X} size={20} />
            </Primitive.Close>
          </div>
          <nav className="fy-breadcrumb flex-wrap px-4 pb-2" aria-label="Trace path">
            <button type="button" className={linkButton} onClick={onClose}>
              {figure.context}
            </button>
            {crumbs.map((c, i) => (
              <span key={c.level} className="fy-row gap-1">
                <Icon of={ChevronRight} />
                {i === crumbs.length - 1 ? (
                  <span aria-current="page" className="fy-strong">
                    {c.label}
                  </span>
                ) : (
                  <button type="button" className={linkButton} onClick={() => open(c.level)}>
                    {c.label}
                  </button>
                )}
              </span>
            ))}
          </nav>
          <div className="fy-tabs px-3" role="tablist" aria-label="Level">
            {(["breakdown", "lines", "source", "history"] as const).map((l, i) => (
              <button key={l} type="button" role="tab" aria-selected={level === l} disabled={!reachable[l]} onClick={() => open(l)}>
                {i + 1} {levelNames[l]}
              </button>
            ))}
          </div>
          <div className="flex-auto overflow-auto" role="tabpanel" aria-label={levelNames[level]}>
            {level === "breakdown" && (
              <ul className="fy-list" aria-label={`The accounts of ${figure.title}`}>
                {figure.accounts.map((a) => (
                  <li key={a.id}>
                    <button
                      type="button"
                      className="fy-list__item w-full text-left"
                      onClick={() => {
                        setAccount(a);
                        setLine(undefined);
                        setDocumentId(undefined);
                        open("lines");
                      }}
                    >
                      <span className="fy-list__main">
                        <span className="fy-list__title">
                          <span className="fy-mono fy-muted fy-expert-only">{a.code} </span>
                          {a.name}
                        </span>
                      </span>
                      {a.amount !== undefined && <span className="fy-num">{formatAmount(a.amount)}</span>}
                    </button>
                  </li>
                ))}
              </ul>
            )}
            {level === "lines" && account && (
              <Lines
                figure={figure}
                account={account}
                onLine={(l) => {
                  setLine(l);
                  setDocumentId(undefined);
                  open("source");
                }}
              />
            )}
            {level === "source" && line && (
              <Source
                entryId={line.entry_id}
                onHistory={() => {
                  setDocumentId(line.source_id);
                  open("history");
                }}
              />
            )}
            {level === "history" && documentId && <History documentId={documentId} />}
          </div>
          <div className="fy-row fy-small fy-muted gap-2 border-t border-border bg-surface-2 px-4 py-3">
            <Icon of={GitCommitHorizontal} />
            <span className="fy-grow">Read at ledger revision {figure.revision}</span>
            <span className="fy-from-medium">Back keeps your place</span>
          </div>
        </Primitive.Popup>
      </Primitive.Portal>
    </Primitive.Root>
  );
}
