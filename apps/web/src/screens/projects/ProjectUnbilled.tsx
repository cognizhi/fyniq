import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { FilePlus2 } from "lucide-react";
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
import { hours } from "./data";

// Those who can make a draft invoice: the roles that create invoices (Appendix D), the same as the invoice command.
const billers = ["accountant", "ar_specialist"];

// ProjectUnbilled is what a project can bill now (FR-PRJ-10): approved billable time at billing rates and posted billable costs at cost plus
// markup, with the draft invoice. Posting the invoice marks the work billed; voiding it releases it.
export function ProjectUnbilled({ project }: { project: string }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const navigate = useNavigate();
  const { session } = useSignedIn();
  const can = (session.roles ?? []).some((r) => billers.includes(r));
  const [drafting, setDrafting] = useState(false);
  const [account, setAccount] = useState("");
  const [gst, setGst] = useState("SR");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const q = useQuery({
    queryKey: ["project-unbilled", project],
    retry: false,
    queryFn: async () => {
      const r = await api.GET("/v1/projects/{project_id}/unbilled", { params: { path: { project_id: project } } });
      if (r.error) throw r.error;
      return r.data;
    },
  });
  const accounts = useQuery({ queryKey: ["accounts", "revenue"], enabled: drafting, queryFn: listAccounts });
  const h = helpText("project.unbilled");
  const u = q.data;
  const money = (v: string) => `${u?.currency ?? ""} ${formatAmount(v)}`;
  const close = () => {
    setDrafting(false);
    setProblem(null);
    setErrors({});
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const errs: Record<string, string> = {};
    if (!account) errs.account = "Choose the revenue account the invoice's lines earn.";
    if (!/^[A-Z]{2,4}$/.test(gst.trim())) errs.gst = "Enter the GST code, such as SR.";
    setErrors(errs);
    if (Object.keys(errs).length) return;
    setBusy(true);
    setProblem(null);
    const key = newKey();
    const out = await guarded(confirm, () =>
      api.POST("/v1/project-invoice-drafts", { params: { header: { "Idempotency-Key": key } }, body: { project_id: project, revenue_account_id: account, gst_code: gst.trim() } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    await queries.invalidateQueries({ queryKey: ["project-unbilled", project] });
    close();
    void navigate({ to: "/sales/invoices/$invoiceId", params: { invoiceId: out.data.id } });
  };
  if (q.isError) {
    return problemOf(q.error).code === "PRJ-09" ? null : <Refusal problem={problemOf(q.error)} title="The unbilled work could not be read" />;
  }
  if (!u || u.billing_method === "internal") return null;
  const ready = u.time.length + u.expenses.length > 0;
  return (
    <>
      <section className="fy-card" aria-labelledby={`${id}-u`}>
        <div className="fy-card__head">
          <h2 className="fy-h3" id={`${id}-u`} title={h.tooltip}>
            {h.label}
          </h2>
          {can && u.has_customer && (
            <Button size="sm" disabled={!ready} onClick={() => setDrafting(true)}>
              <Icon of={FilePlus2} />
              Draft an invoice
            </Button>
          )}
        </div>
        <div className="fy-card__body fy-stack fy-stack--2">
          <p className="fy-num">
            {money(u.total.amount)} to bill: time {money(u.time_value.amount)}, costs {money(u.expense_value.amount)}.
          </p>
          {!u.has_customer && <p className="fy-banner fy-banner--warning">The project has no customer, so no invoice can be drafted. Set its customer first.</p>}
          {u.billing_method !== "time_and_materials" && <p className="fy-small fy-muted">Only costs bill here: a fixed fee bills by milestone, and a retainer draws time down.</p>}
          {u.unpriced_time.length > 0 && (
            <p className="fy-banner fy-banner--warning" role="status">
              {u.unpriced_time.length} approved entr{u.unpriced_time.length === 1 ? "y has" : "ies have"} no billing rate, so they cannot be invoiced. Enter a billing rate from the first day worked.
            </p>
          )}
          {u.time.length > 0 && (
            <table className="fy-table fy-table--compact" aria-label="Unbilled time">
              <thead>
                <tr>
                  <th scope="col">Day</th>
                  <th scope="col">Person</th>
                  <th scope="col" className="fy-num">Hours</th>
                  <th scope="col" className="fy-num">Rate an hour</th>
                  <th scope="col" className="fy-num">Value</th>
                </tr>
              </thead>
              <tbody>
                {u.time.map((t) => (
                  <tr key={t.entry_id}>
                    <td>{date(t.work_date)}</td>
                    <th scope="row">{t.person_name}</th>
                    <td className="fy-num">{hours(t.minutes)}</td>
                    <td className="fy-num">{t.rate ? formatAmount(t.rate) : ""}</td>
                    <td className="fy-num">{t.value ? formatAmount(t.value) : ""}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          {u.expenses.length > 0 && (
            <table className="fy-table fy-table--compact" aria-label="Unbilled costs">
              <thead>
                <tr>
                  <th scope="col">Cost</th>
                  <th scope="col">On</th>
                  <th scope="col" className="fy-num">Cost</th>
                  <th scope="col" className="fy-num">Markup</th>
                  <th scope="col" className="fy-num">Billed</th>
                </tr>
              </thead>
              <tbody>
                {u.expenses.map((x) => (
                  <tr key={`${x.document_id}-${x.line}`}>
                    <th scope="row">{x.label || "Cost"}</th>
                    <td>
                      {x.source === "bill" ? "Bill" : "Claim"} {x.number}
                    </td>
                    <td className="fy-num">{formatAmount(x.cost.amount)}</td>
                    <td className="fy-num">{x.markup_percent ? `${formatAmount(x.markup.amount)} (${x.markup_percent}%)` : ""}</td>
                    <td className="fy-num">{formatAmount(x.billable.amount)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          {u.expenses_in_other_currencies.length > 0 && (
            <p className="fy-small fy-muted">
              {u.expenses_in_other_currencies.length} billable cost(s) are in another currency than the project's and are not on its invoice.
            </p>
          )}
          {!ready && u.unpriced_time.length === 0 && <p className="fy-secondary">Nothing is waiting to be billed.</p>}
        </div>
      </section>
      <Dialog open={drafting} onOpenChange={(o) => !o && close()} dismissible={!busy} eyebrow="Unbilled work" title="Draft an invoice from the unbilled work" description="A draft invoice to the project's customer, one line a person for the time and one a cost. Nothing is billed until you issue it; issuing marks the work billed, and voiding the invoice releases it.">
        <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-a`}>
              Revenue account
            </label>
            <select className="fy-select" id={`${id}-a`} value={account} onChange={(e) => setAccount(e.target.value)} aria-invalid={Boolean(errors.account) || undefined} aria-describedby={errors.account ? `${id}-a-err` : undefined}>
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
              <span className="fy-error" id={`${id}-a-err`}>
                {errors.account}
              </span>
            )}
          </div>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-g`}>
              GST code
            </label>
            <input className="fy-input" id={`${id}-g`} value={gst} maxLength={4} onChange={(e) => setGst(e.target.value.toUpperCase())} aria-invalid={Boolean(errors.gst) || undefined} aria-describedby={errors.gst ? `${id}-g-err` : `${id}-g-help`} />
            {errors.gst ? (
              <span className="fy-error" id={`${id}-g-err`}>
                {errors.gst}
              </span>
            ) : (
              <span className="fy-help" id={`${id}-g-help`}>
                SR for standard-rated; choose another where it applies.
              </span>
            )}
          </div>
          {problem && <Refusal problem={problem} title="The invoice could not be drafted" />}
          <div className="fy-row gap-3">
            <Button type="submit" disabled={busy}>
              Draft the invoice
            </Button>
            <Button type="button" variant="ghost" onClick={close}>
              Cancel
            </Button>
          </div>
        </form>
      </Dialog>
    </>
  );
}
