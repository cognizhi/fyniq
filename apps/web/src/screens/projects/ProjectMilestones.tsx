import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { FilePlus2, Plus } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { listAccounts } from "../accounting/data";

const billers = ["accountant", "ar_specialist"];

// ProjectMilestones are a fixed-fee project's milestones, or a retainer's periods and what time has drawn down (FR-PRJ-10): add one, record the
// customer's acceptance of a milestone, and draft the invoice that bills it. Posting the invoice marks it billed; voiding it releases it.
export function ProjectMilestones({ project, method }: { project: string; method: "fixed_fee" | "retainer" }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const navigate = useNavigate();
  const { session } = useSignedIn();
  const can = (session.roles ?? []).some((r) => billers.includes(r));
  const retainer = method === "retainer";
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState("");
  const [amount, setAmount] = useState("");
  const [due, setDue] = useState("");
  const [billing, setBilling] = useState<string | null>(null);
  const [account, setAccount] = useState("");
  const [gst, setGst] = useState("SR");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const q = useQuery({
    queryKey: ["project-milestones", project],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/projects/{project_id}/milestones", { params: { path: { project_id: project }, query: { page_size: 500 } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const accounts = useQuery({ queryKey: ["accounts", "revenue"], enabled: billing !== null, queryFn: listAccounts });
  const h = helpText("project.milestones");
  const d = q.data;
  const closeAdd = () => {
    setAdding(false);
    setName("");
    setAmount("");
    setDue("");
    setErrors({});
    setProblem(null);
  };
  const add = async (e: FormEvent) => {
    e.preventDefault();
    const errs: Record<string, string> = {};
    if (!name.trim()) errs.name = retainer ? "Name the period, such as November." : "Name the milestone, such as Design sign-off.";
    if (!/^\d+(\.\d{1,4})?$/.test(amount.trim()) || Number(amount) <= 0) errs.amount = "Write what it bills as a number above zero, such as 15000.00.";
    setErrors(errs);
    if (Object.keys(errs).length) return;
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/project-milestones", { params: { header: { "Idempotency-Key": key } }, body: { project_id: project, name: name.trim(), amount: amount.trim(), due_on: due || undefined } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["project-milestones", project] });
    closeAdd();
  };
  const accept = async (m: { id: string; version: number }) => {
    const key = newKey();
    const today = new Date().toISOString().slice(0, 10);
    const out = await guarded(confirm, () =>
      api.POST("/v1/project-milestones/{milestone_id}/accept", {
        params: { path: { milestone_id: m.id }, header: { "If-Match": `"${m.version}"`, "Idempotency-Key": key } },
        body: { accepted_on: today },
      }),
    );
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["project-milestones", project] });
  };
  const draft = async (e: FormEvent) => {
    e.preventDefault();
    const errs: Record<string, string> = {};
    if (!account) errs.account = "Choose the revenue account the invoice's line earns.";
    if (!/^[A-Z]{2,4}$/.test(gst.trim())) errs.gst = "Enter the GST code, such as SR.";
    setErrors(errs);
    if (Object.keys(errs).length || !billing) return;
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/project-invoice-drafts", { params: { header: { "Idempotency-Key": key } }, body: { project_id: project, revenue_account_id: account, gst_code: gst.trim(), milestone_id: billing } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    setBilling(null);
    await queries.invalidateQueries({ queryKey: ["project-milestones", project] });
    void navigate({ to: "/sales/invoices/$invoiceId", params: { invoiceId: out.data.id } });
  };
  if (q.isError) return problemOf(q.error).code === "PRJ-09" ? null : <Refusal problem={problemOf(q.error)} title="The milestones could not be read" />;
  if (!d) return null;
  const money = (v: { amount: string; currency: string }) => `${v.currency} ${formatAmount(v.amount)}`;
  return (
    <>
      <section className="fy-card" aria-labelledby={`${id}-m`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-m`} title={h.tooltip}>
            {retainer ? "Retainer periods" : h.label}
          </h2>
          {can && (
            <Button size="sm" variant="secondary" onClick={() => setAdding(true)}>
              <Icon of={Plus} />
              {retainer ? "Add a period" : "Add a milestone"}
            </Button>
          )}
        </div>
        <div className="fy-card__body fy-stack fy-stack--2">
          {d.retainer && (
            <p className="fy-num">
              Billed {money(d.retainer.billed)}, drawn down by time {money(d.retainer.drawn)}, {money(d.retainer.left)} left.
              {d.retainer.unpriced_entries > 0 ? ` ${d.retainer.unpriced_entries} approved entries have no billing rate and are not in the drawn amount.` : ""}
            </p>
          )}
          {problem && <Refusal problem={problem} title="That could not be done" />}
          {d.milestones.length === 0 ? (
            <p className="fy-secondary">{retainer ? "No retainer period yet." : "No milestone yet. Add the parts of the fixed fee and invoice each once the customer accepts it."}</p>
          ) : (
            <table className="fy-table fy-table--compact" aria-label={retainer ? "Retainer periods" : "Milestones"}>
              <thead>
                <tr>
                  <th scope="col">{retainer ? "Period" : "Milestone"}</th>
                  <th scope="col" className="fy-num">Amount</th>
                  <th scope="col">Due</th>
                  <th scope="col">Status</th>
                  <th scope="col">
                    <span className="fy-sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {d.milestones.map((m) => (
                  <tr key={m.id}>
                    <th scope="row">{m.name}</th>
                    <td className="fy-num">{formatAmount(m.amount.amount)}</td>
                    <td>{m.due_on ? date(m.due_on) : ""}</td>
                    <td>{m.invoice_id ? "Invoiced" : m.accepted_on ? `Accepted ${date(m.accepted_on)}` : retainer ? "Ready to invoice" : "Awaiting acceptance"}</td>
                    <td>
                      {can && !retainer && !m.accepted_on && (
                        <Button size="sm" variant="ghost" onClick={() => void accept(m)}>
                          Record acceptance
                        </Button>
                      )}
                      {can && m.can_invoice && (
                        <Button size="sm" variant="ghost" onClick={() => setBilling(m.id)}>
                          <Icon of={FilePlus2} />
                          Draft an invoice
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </section>
      <Dialog open={adding} onOpenChange={(o) => !o && closeAdd()} dismissible={!busy} eyebrow={retainer ? "Retainer" : "Fixed fee"} title={retainer ? "Add a retainer period" : "Add a milestone"} description="Its name and amount never change. The amount is in the project's currency.">
        <form className="fy-stack fy-stack--3" onSubmit={add} noValidate>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-n`}>
              {retainer ? "Period" : "Milestone"}
            </label>
            <input className="fy-input" id={`${id}-n`} value={name} maxLength={100} onChange={(e) => setName(e.target.value)} aria-invalid={Boolean(errors.name) || undefined} aria-describedby={errors.name ? `${id}-n-err` : undefined} />
            {errors.name && (
              <span className="fy-error" id={`${id}-n-err`}>
                {errors.name}
              </span>
            )}
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-a`}>
              Amount
            </label>
            <input className="fy-input fy-num" id={`${id}-a`} inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} aria-invalid={Boolean(errors.amount) || undefined} aria-describedby={errors.amount ? `${id}-a-err` : undefined} />
            {errors.amount && (
              <span className="fy-error" id={`${id}-a-err`}>
                {errors.amount}
              </span>
            )}
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-d`}>
              Due (optional)
            </label>
            <input className="fy-input" id={`${id}-d`} type="date" value={due} onChange={(e) => setDue(e.target.value)} />
          </div>
          {problem && <Refusal problem={problem} title="The milestone was not saved" />}
          <div className="fy-row gap-3">
            <Button type="submit" disabled={busy}>
              Save
            </Button>
            <Button type="button" variant="ghost" onClick={closeAdd}>
              Cancel
            </Button>
          </div>
        </form>
      </Dialog>
      <Dialog open={billing !== null} onOpenChange={(o) => !o && setBilling(null)} dismissible={!busy} eyebrow="Invoice" title="Draft an invoice for this milestone" description="A draft invoice to the project's customer with one line at the milestone's amount. Issuing it marks the milestone billed; voiding the invoice releases it.">
        <form className="fy-stack fy-stack--3" onSubmit={draft} noValidate>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-r`}>
              Revenue account
            </label>
            <select className="fy-select" id={`${id}-r`} value={account} onChange={(e) => setAccount(e.target.value)} aria-invalid={Boolean(errors.account) || undefined} aria-describedby={errors.account ? `${id}-r-err` : undefined}>
              <option value="">Choose the account</option>
              {(accounts.data ?? [])
                .filter((a) => a.type === "revenue" && !a.group && a.active)
                .map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.code} {a.name}
                  </option>
                ))}
            </select>
            {errors.account && (
              <span className="fy-error" id={`${id}-r-err`}>
                {errors.account}
              </span>
            )}
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-g`}>
              GST code
            </label>
            <input className="fy-input" id={`${id}-g`} value={gst} maxLength={4} onChange={(e) => setGst(e.target.value.toUpperCase())} aria-invalid={Boolean(errors.gst) || undefined} aria-describedby={errors.gst ? `${id}-g-err` : undefined} />
            {errors.gst && (
              <span className="fy-error" id={`${id}-g-err`}>
                {errors.gst}
              </span>
            )}
          </div>
          {problem && <Refusal problem={problem} title="The invoice could not be drafted" />}
          <div className="fy-row gap-3">
            <Button type="submit" disabled={busy}>
              Draft the invoice
            </Button>
            <Button type="button" variant="ghost" onClick={() => setBilling(null)}>
              Cancel
            </Button>
          </div>
        </form>
      </Dialog>
    </>
  );
}
