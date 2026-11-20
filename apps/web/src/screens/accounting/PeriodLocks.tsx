import { useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Lock, LockKeyhole, LockOpen } from "lucide-react";
import { useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import type { Period } from "./data";

type Exception = components["schemas"]["SoftLockException"];
type Module = "ar" | "ap" | "banking" | "payroll";

// Who may do what, by the rights of "Period close / reopen" (Appendix D; the server decides, this only hides what it would refuse):
// locking is C (the Accountant and the Controller), releasing is A (the Owner and the Controller), and a pass is the Accountant's alone.
export const lockers = ["accountant", "controller"];
export const releasers = ["owner", "controller"];
export const passMakers = ["accountant"];

const modules: { id: Module; label: string; what: string }[] = [
  { id: "ar", label: "Sales", what: "invoices, credit notes, receipts and refunds" },
  { id: "ap", label: "Purchases", what: "bills, supplier payments, claims and credits" },
  { id: "banking", label: "Banking", what: "bank lines and transfers" },
  { id: "payroll", label: "Payroll", what: "pay runs" },
];

// The period's ETag, as the API makes it.
const tagOf = (p: Period) => `"${p.version}-${p.status}"`;

export function SoftLockChip({ p }: { p: Period }) {
  return (
    <>
      <span className="fy-chip fy-chip--warning">
        <Icon of={LockKeyhole} />
        Soft-locked
      </span>
      <span className="fy-sr"> {p.locked_modules?.length ? "" : "Only an Accountant with a pass can post"}</span>
    </>
  );
}

export function ModuleChips({ p }: { p: Period }) {
  return (
    <>
      {(p.locked_modules ?? []).map((m) => (
        <span key={m} className="fy-chip fy-chip--info">
          <Icon of={Lock} />
          {modules.find((x) => x.id === m)?.label ?? m} locked
        </span>
      ))}
    </>
  );
}

// LocksDialog is Accounting › Periods › Locks (FR-GL-09): soft-lock a month or one module of it, release a lock, and, for an Accountant, make a pass that lets them post into
// a locked month for a few hours, with a reason the exceptions report keeps. The hard close is not here: it is the close page.
export function LocksDialog({ period, roles, onClose, onDone }: { period: Period | null; roles: string[]; onClose: () => void; onDone: (said: string) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const id = useId();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [reason, setReason] = useState("");
  const [hours, setHours] = useState("4");
  const may = (list: string[]) => roles.some((r) => list.includes(r));
  const lockHelp = helpText("period.soft-lock");
  const passHelp = helpText("period.soft-lock-pass");
  const locked = (period?.locked_modules ?? []) as string[];

  const run = async (said: string, call: () => Promise<{ error?: unknown }>) => {
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, call);
    setBusy(false);
    if (out.error) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["periods"] });
    await queries.invalidateQueries({ queryKey: ["soft-lock-exceptions"] });
    setReason("");
    onDone(said);
    onClose();
  };
  if (!period) return <Dialog open={false} onOpenChange={() => undefined} title="" description="" />;
  const path = { period_id: period.id };
  const name = new Date(`${period.starts_on}T00:00:00Z`).toLocaleString("en-SG", { month: "long", year: "numeric", timeZone: "UTC" });
  const key = newKey();
  const header = { "Idempotency-Key": key };
  const why = reason.trim();

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={`Locks for ${name}`}
      description="A soft lock stops postings from everyone except an Accountant who holds a pass, which is made with a reason and listed on the exceptions report. The close is separate and stricter."
      footer={
        <Button variant="ghost" onClick={onClose} disabled={busy}>
          Close
        </Button>
      }
    >
      {problem && <Refusal problem={problem} title="The lock was not changed" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-reason`}>
          Why (kept in the audit trail; 10 characters or more to release a lock or make a pass)
        </label>
        <textarea className="fy-textarea" id={`${id}-reason`} value={reason} maxLength={500} onChange={(e) => setReason(e.target.value)} />
      </div>

      <section className="fy-stack fy-stack--2" aria-labelledby={`${id}-month`}>
        <h3 className="fy-h3" id={`${id}-month`}>
          The whole month
        </h3>
        {period.status === "open" && (
          <Button
            variant="secondary"
            disabled={busy || !may(lockers)}
            title={lockHelp.tooltip}
            onClick={() =>
              void run(`${name} is soft-locked.`, () =>
                api.POST("/v1/periods/{period_id}/soft-lock", { params: { path, header: { ...header, "If-Match": tagOf(period) } }, body: why ? { reason: why } : {} }),
              )
            }
          >
            <Icon of={LockKeyhole} />
            {lockHelp.label}
          </Button>
        )}
        {period.status === "soft_locked" && (
          <Button
            variant="secondary"
            disabled={busy || !may(releasers) || why.length < 10}
            onClick={() =>
              void run(`${name} is open again.`, () =>
                api.POST("/v1/periods/{period_id}/unlock", { params: { path, header: { ...header, "If-Match": tagOf(period) } }, body: { reason: why } }),
              )
            }
          >
            <Icon of={LockOpen} />
            Release the soft lock
          </Button>
        )}
        {!may(period.status === "open" ? lockers : releasers) && (
          <p className="fy-small fy-secondary">{period.status === "open" ? "An Accountant or a Controller locks a month." : "An Owner or a Controller releases a soft lock."}</p>
        )}
      </section>

      <section className="fy-stack fy-stack--2" aria-labelledby={`${id}-modules`}>
        <h3 className="fy-h3" id={`${id}-modules`}>
          By module
        </h3>
        <ul className="fy-list">
          {modules.map((m) => {
            const on = locked.includes(m.id);
            return (
              <li key={m.id} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title">{m.label}</span>
                  <span className="fy-list__meta whitespace-normal">{m.what}</span>
                </span>
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={busy || (on ? !may(releasers) || why.length < 10 : !may(lockers))}
                  aria-label={`${on ? "Release" : "Lock"} ${m.label.toLowerCase()} for ${name}`}
                  onClick={() =>
                    void run(`${m.label} is ${on ? "released" : "locked"} for ${name}.`, () =>
                      api.POST(on ? "/v1/periods/{period_id}/modules/{module}/release" : "/v1/periods/{period_id}/modules/{module}/lock", {
                        params: { path: { ...path, module: m.id }, header },
                        body: why ? { reason: why } : {},
                      }),
                    )
                  }
                >
                  <Icon of={on ? LockOpen : Lock} />
                  {on ? "Release" : "Lock"}
                </Button>
              </li>
            );
          })}
        </ul>
      </section>

      {may(passMakers) && period.status !== "closed" && (
        <section className="fy-stack fy-stack--2" aria-labelledby={`${id}-pass`}>
          <h3 className="fy-h3" id={`${id}-pass`}>
            Post anyway
          </h3>
          <p className="fy-small fy-secondary">A pass lets only you post into this month, for the hours you choose. Every posting made under it is on the exceptions report with your reason.</p>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-hours`}>
              Hours
            </label>
            <select className="fy-select" id={`${id}-hours`} value={hours} onChange={(e) => setHours(e.target.value)}>
              {[1, 2, 4, 8].map((h) => (
                <option key={h} value={h}>
                  {h}
                </option>
              ))}
            </select>
          </div>
          <Button
            variant="secondary"
            disabled={busy || why.length < 10}
            title={passHelp.tooltip}
            onClick={() =>
              void run(`You hold a pass for ${name} for ${hours} hours.`, () =>
                api.POST("/v1/periods/{period_id}/soft-lock-passes", { params: { path, header }, body: { reason: why, hours: Number(hours) } }),
              )
            }
          >
            <Icon of={KeyRound} />
            {passHelp.label}
          </Button>
        </section>
      )}
    </Dialog>
  );
}

// Exceptions is the exceptions report of FR-GL-09: the postings made into a locked month under a pass, newest first.
export function Exceptions() {
  const q = useQuery({
    queryKey: ["soft-lock-exceptions"],
    retry: false,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/soft-lock-exceptions", { params: { query: { page_size: 100 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  if (q.isError || !q.data) return null; // a role without the right to see the periods' locks sees no report
  const items: Exception[] = q.data;
  return (
    <section className="fy-card" aria-labelledby="soft-lock-exceptions">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="soft-lock-exceptions">
          Postings made into locked months
        </h2>
      </div>
      {items.length === 0 ? (
        <p className="fy-secondary fy-card--pad">None yet. A posting an Accountant makes under a pass appears here with the reason.</p>
      ) : (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--compact" aria-labelledby="soft-lock-exceptions">
            <thead>
              <tr>
                <th scope="col">Dated</th>
                <th scope="col">Kind</th>
                <th scope="col" className="fy-from-medium">
                  Locked
                </th>
                <th scope="col">Reason</th>
              </tr>
            </thead>
            <tbody>
              {items.map((x) => (
                <tr key={x.id}>
                  <td>{date(x.posting_date)}</td>
                  <td>{x.source_type.replace(/_/g, " ")}</td>
                  <td className="fy-from-medium">{x.scope === "period" ? "The month" : x.scope}</td>
                  <td className="whitespace-normal">{x.reason}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
