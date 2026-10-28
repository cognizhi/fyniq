import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Ban, CircleCheck, Hourglass, PencilLine, Plus, Send, TriangleAlert, UserCheck, Wallet } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
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
import { BillTabs } from "./Bills";

export type Batch = components["schemas"]["PaymentBatch"];

// Who acts on payment batches (Appendix D's "Payment batches"): the Accountant and the AP specialist prepare them (C), the
// Controller approves them (A) and the Payment releaser releases them (R). The API decides.
export const batchPreparers = ["accountant", "ap_specialist"];
export const batchReleasers = ["payment_releaser"];

// BatchStatus is a batch's status chip: icon and label, never colour alone.
export function BatchStatus({ batch }: { batch: Batch }) {
  if (batch.status === "approved" && batch.reapproval_needed)
    return (
      <span className="fy-chip fy-chip--critical">
        <Icon of={TriangleAlert} />
        Needs approving again
      </span>
    );
  switch (batch.status) {
    case "draft":
      return (
        <span className="fy-chip fy-chip--neutral">
          <Icon of={PencilLine} />
          Draft
        </span>
      );
    case "waiting":
      return (
        <span className="fy-chip fy-chip--info">
          <Icon of={UserCheck} />
          Awaiting approval
        </span>
      );
    case "approved":
      return (
        <span className="fy-chip fy-chip--good">
          <Icon of={CircleCheck} />
          Approved
        </span>
      );
    case "released":
      return (
        <span className="fy-chip fy-chip--serious">
          <Icon of={Send} />
          Released, not yet paid
        </span>
      );
    case "paid":
      return (
        <span className="fy-chip fy-chip--good">
          <Icon of={Wallet} />
          Paid
        </span>
      );
  }
  return (
    <span className="fy-chip fy-chip--neutral">
      <Icon of={Ban} />
      Cancelled
    </span>
  );
}

// batchName is how people name a batch: its payment date, and its number once approved.
export function batchName(b: Batch): string {
  return `${date(b.payment_date)} batch${b.number ? ` ${b.number}` : ""}`;
}

const filters: [Batch["status"] | "", string][] = [
  ["", "All"],
  ["draft", "Drafts"],
  ["waiting", "Awaiting approval"],
  ["approved", "Approved"],
  ["released", "Released"],
  ["paid", "Paid"],
];

function today(): string {
  return new Date().toLocaleDateString("en-CA");
}

// NewBatch prepares a batch for a payment date from an SGD bank account (FR-BNK-12, FR-AP-13): it starts with the bills
// planned on or before the date and the claims owed, which the batch's page then lets you keep or remove.
function NewBatch({ open, onClose }: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate();
  const formId = useId();
  const h = helpText("batch.new");
  const [account, setAccount] = useState("");
  const [day, setDay] = useState(today());
  const [method, setMethod] = useState<"fast" | "giro">("fast");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const accounts = useQuery({
    queryKey: ["money-accounts", "batch"],
    enabled: open,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/money-accounts", { params: { query: { page_size: 100 } } });
      if (error) throw error;
      return (data.items ?? []).filter((m) => m.kind === "bank" && m.currency === "SGD" && !m.closed_on);
    },
  });
  const chosen = account || accounts.data?.[0]?.account_id || "";
  const id = (k: string) => `${formId}-${k}`;

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const suggestion = await api.GET("/v1/payment-batches/suggestion", { params: { query: { payment_date: day } } });
    if (!suggestion.data) {
      setBusy(false);
      return setProblem(problemOf(suggestion.error));
    }
    const out = await api.POST("/v1/payment-batches", {
      params: { header: { "Idempotency-Key": newKey() } },
      body: { money_account_id: chosen, payment_date: day, method, items: suggestion.data.suggested },
    });
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onClose();
    await navigate({ to: "/purchases/payment-batches/$batchId", params: { batchId: out.data.id } });
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      eyebrow="Payment batches"
      title={h.label}
      description="It starts with the bills planned on or before the date and the claims owed. Keep or remove each one on the next page; nothing is paid until another person approves it and it is released."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" form={formId} disabled={busy || !chosen} title={h.tooltip}>
            <Icon of={Plus} />
            Prepare the batch
          </Button>
        </>
      }
    >
      <div className="fy-stack fy-stack--3">
        {problem && <Refusal problem={problem} title="The batch was not prepared" />}
        {accounts.isError && <Refusal problem={problemOf(accounts.error)} title="The bank accounts could not be listed" />}
        {accounts.data && accounts.data.length === 0 && (
          <p className="fy-small">No SGD bank account is set up in Banking yet. A batch pays SGD by FAST or GIRO from one.</p>
        )}
        <form className="fy-form fy-form--2" id={formId} onSubmit={(e) => void create(e)} noValidate>
          <div className="fy-field">
            <label className="fy-label" htmlFor={id("account")}>
              Pay from
            </label>
            <select className="fy-select" id={id("account")} value={chosen} onChange={(e) => setAccount(e.target.value)}>
              {(accounts.data ?? []).map((m) => (
                <option key={m.id} value={m.account_id}>
                  {m.name}
                  {m.identifier ? ` (${m.identifier})` : ""}
                </option>
              ))}
            </select>
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={id("date")}>
              Payment date
            </label>
            <input className="fy-input" id={id("date")} type="date" value={day} onChange={(e) => setDay(e.target.value)} />
          </div>
          <fieldset className="fy-field fy-field--full m-0 min-w-0 border-0 p-0">
            <legend className="fy-label">How the bank pays it</legend>
            <div className="fy-row fy-row--wrap gap-3">
              <label className="fy-row gap-2">
                <input type="radio" name={id("method")} checked={method === "fast"} onChange={() => setMethod("fast")} />
                FAST, within minutes
              </label>
              <label className="fy-row gap-2">
                <input type="radio" name={id("method")} checked={method === "giro"} onChange={() => setMethod("giro")} />
                GIRO, over the next working days
              </label>
            </div>
          </fieldset>
        </form>
      </div>
    </Dialog>
  );
}

// PaymentBatches is Purchases › Bills › Batches (docs/design/screens PaymentBatches; FR-BNK-12; PRD-001 rev 0.154 and rev
// 0.156): the batches that pay suppliers and claimants, from prepared to paid, each step by a different person.
export function PaymentBatches() {
  const { session } = useSignedIn();
  const [status, setStatus] = useState<Batch["status"] | "">("");
  const [adding, setAdding] = useState(false);
  const list = useInfiniteQuery({
    queryKey: ["payment-batches", status],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/v1/payment-batches", { params: { query: { page_size: 50, cursor: pageParam, status: status || undefined } } });
      if (error) throw error;
      return data;
    },
    getNextPageParam: (last) => last.next_cursor,
  });
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  const may = (session.roles ?? []).some((r) => batchPreparers.includes(r));
  const add = helpText("batch.new");
  const blocked = items.filter((b) => b.status === "approved" && b.reapproval_needed);

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <span>Purchases</span>
          </nav>
          <h1 className="fy-h1">Bills</h1>
          <p className="fy-secondary">Supplier and claim payments, from prepared to paid. Each step is done by a different person.</p>
        </div>
        {may && (
          <div className="fy-pagehead__actions">
            <Button variant="primary" onClick={() => setAdding(true)} title={add.tooltip}>
              <Icon of={Plus} />
              {add.label}
            </Button>
          </div>
        )}
      </div>
      <ReviewBanner screen="payment-batches" />
      <BillTabs />
      {blocked.map((b) => (
        <div key={b.id} className="fy-banner fy-banner--critical" role="status">
          <Icon of={TriangleAlert} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">{batchName(b)} needs approving again</span>
            <span className="fy-small">A payee's bank account, or what is open on an item, changed after it was approved. It can't be released until it is approved again.</span>
          </div>
          <Link className="fy-btn fy-btn--secondary fy-btn--sm" to="/purchases/payment-batches/$batchId" params={{ batchId: b.id }}>
            Open batch
          </Link>
        </div>
      ))}
      <div className="fy-chipset" role="group" aria-label="Status">
        {filters.map(([s, label]) => (
          <button key={label} type="button" className="fy-filter" aria-pressed={status === s} onClick={() => setStatus(s)}>
            {label}
          </button>
        ))}
      </div>
      {list.isError && <Refusal problem={problemOf(list.error)} title="The payment batches could not be listed" />}
      {list.isPending && <p className="fy-secondary">Loading the payment batches…</p>}
      {list.data && items.length === 0 && (
        <div className="fy-empty fy-card fy-card--pad">
          <Icon of={Wallet} size={24} />
          <h2 className="fy-h3">No payment batches {status ? "here" : "yet"}</h2>
          <p className="fy-secondary">A batch pays approved bills and claims from one SGD bank account on one date: one payment per supplier, one per claimant.</p>
          {!may && <p className="fy-small fy-secondary">An Accountant or an AP specialist prepares batches.</p>}
        </div>
      )}
      {items.length > 0 && (
        <div className="fy-tablewrap">
          <table className="fy-table fy-table--cards" aria-label="Payment batches">
            <thead>
              <tr>
                <th scope="col">Batch</th>
                <th scope="col">Pays</th>
                <th scope="col" className="fy-num">
                  Total SGD
                </th>
                <th scope="col">Status</th>
              </tr>
            </thead>
            <tbody>
              {items.map((b) => (
                <tr key={b.id}>
                  <td data-card="title">
                    <Link to="/purchases/payment-batches/$batchId" params={{ batchId: b.id }}>
                      {batchName(b)}
                    </Link>
                    <span className="fy-small fy-muted block">{b.money_account_name ?? ""}</span>
                  </td>
                  <td data-card="meta">
                    {b.payments.length === 1 ? "1 payment" : `${b.payments.length} payments`} · {b.method.toUpperCase()}
                  </td>
                  <td data-card="amount" className="fy-num">
                    {formatAmount(b.total.amount)}
                  </td>
                  <td data-card="status">
                    <BatchStatus batch={b} />
                  </td>
                </tr>
              ))}
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
      <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby="how-title">
        <h2 className="fy-h3" id="how-title">
          How a batch is paid
        </h2>
        <ol className="fy-stack fy-stack--2 fy-small fy-secondary">
          <li className="fy-row fy-row--top gap-2">
            <Icon of={PencilLine} />
            Someone prepares it from approved bills and claims.
          </li>
          <li className="fy-row fy-row--top gap-2">
            <Icon of={UserCheck} />
            A different person approves it.
          </li>
          <li className="fy-row fy-row--top gap-2">
            <Icon of={Send} />
            The Payment releaser releases it with a passkey, then pays it in the bank from the payment list.
          </li>
          <li className="fy-row fy-row--top gap-2">
            <Icon of={Hourglass} />
            It is marked paid when the bank statement shows each payment; each supplier is then emailed remittance advice.
          </li>
        </ol>
      </section>
      <NewBatch open={adding} onClose={() => setAdding(false)} />
    </div>
  );
}
