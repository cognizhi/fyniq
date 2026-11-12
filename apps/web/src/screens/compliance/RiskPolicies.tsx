import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowDown, ArrowUp, ChevronRight, ClockAlert, Minus, Pencil, Plus, ShieldCheck, type LucideIcon } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Risk = components["schemas"]["RiskView"];
type RiskInput = components["schemas"]["RiskInput"];
type Level = Risk["likelihood"];

const levels: Record<Level, number> = { low: 1, medium: 2, high: 3 };
const words: Record<Level, string> = { low: "Low", medium: "Medium", high: "High" };
const owners: Record<Risk["owner"], string> = { owner: "Owner", administrator: "Administrator", controller: "Controller", accountant: "Accountant", dpo: "Compliance officer", auditor: "Auditor" };

// rating is a risk's rating from its likelihood and impact: high from 6, medium from 3.
export function rating(r: Pick<Risk, "likelihood" | "impact">): Level {
  const s = levels[r.likelihood] * levels[r.impact];
  return s >= 6 ? "high" : s >= 3 ? "medium" : "low";
}

const chips: Record<Level, { chip: string; icon: LucideIcon }> = {
  high: { chip: "fy-chip--serious", icon: ArrowUp },
  medium: { chip: "fy-chip--warning", icon: Minus },
  low: { chip: "fy-chip--good", icon: ArrowDown },
};

async function risks() {
  const { data, error } = await api.GET("/v1/compliance/risks");
  if (error) throw error;
  return data.items;
}

// RiskPolicies is Compliance › Risks and policies (docs/design/screens RiskPolicies; CMP-06; Expert): the risk register,
// highest rating first, each with its owner, rating, mitigation and next review, an overdue review called out; the
// Compliance officer adds, changes and reviews risks.
export function RiskPolicies() {
  const id = useId();
  const h = helpText("compliance.risks");
  const { session } = useSignedIn();
  const officer = (session.roles ?? []).includes("dpo");
  const list = useQuery({ queryKey: ["risks"], queryFn: risks });
  const [editing, setEditing] = useState<Risk | "new" | null>(null);
  const [reviewing, setReviewing] = useState<Risk | null>(null);
  const all = (list.data ?? []).slice().sort((a, b) => levels[rating(b)] - levels[rating(a)] || Number(a.ref.slice(1)) - Number(b.ref.slice(1)));
  const overdue = all.filter((r) => r.review_overdue);
  return (
    <div className="fy-page fy-enter">
      <nav className="fy-breadcrumb" aria-label="Breadcrumb">
        <Link to="/compliance">Compliance</Link>
        <Icon of={ChevronRight} />
        <span>Risks and policies</span>
      </nav>
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">{h.label}</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
        {officer && (
          <div className="fy-pagehead__actions">
            <Button variant="primary" onClick={() => setEditing("new")} title={helpText("risk.save").tooltip}>
              <Icon of={Plus} />
              Add a risk
            </Button>
          </div>
        )}
      </div>
      {list.isError && <Refusal problem={problemOf(list.error)} title="The risk register could not be shown" />}
      {list.isPending && <p className="fy-small">Loading the risk register…</p>}
      {overdue.length > 0 && (
        <div className="fy-banner fy-banner--warning" role="status">
          <Icon of={ClockAlert} size={20} />
          <div className="fy-banner__text">
            <strong>
              {overdue.length} risk review{overdue.length === 1 ? " is" : "s are"} overdue
            </strong>
            <span className="fy-small">{overdue.map((r) => `${r.ref}, due ${date(r.next_review_on)}`).join("; ")}</span>
          </div>
        </div>
      )}
      {list.data && (
        <section className="fy-card" aria-labelledby={`${id}-risks`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`${id}-risks`}>
              Risk register
            </h2>
            <span className="fy-small fy-muted">{all.length} risks · highest rating first</span>
          </div>
          {all.length === 0 ? (
            <p className="fy-card__body fy-small fy-secondary">No risk is in the register yet.{officer ? " Add the first with Add a risk." : ""}</p>
          ) : (
            <ul className="fy-list" aria-labelledby={`${id}-risks`}>
              {all.map((r) => {
                const level = rating(r);
                return (
                  <li key={r.id} className="fy-list__item">
                    <span className="fy-list__main">
                      <span className="fy-list__title whitespace-normal">
                        <span className="fy-mono fy-muted">{r.ref}</span> {r.title}
                        {r.state === "closed" && <span className="fy-muted"> · closed</span>}
                      </span>
                      <span className="fy-list__meta whitespace-normal">
                        {owners[r.owner]} · likelihood {words[r.likelihood].toLowerCase()}, impact {words[r.impact].toLowerCase()} · {r.mitigation}
                      </span>
                      <span className="fy-list__meta">
                        {r.review_overdue ? (
                          <span className="fy-chip fy-chip--serious">
                            <Icon of={ClockAlert} />
                            Review overdue · {date(r.next_review_on)}
                          </span>
                        ) : (
                          `Next review ${date(r.next_review_on)}`
                        )}
                        {r.last_review && ` · last reviewed ${date(r.last_review.reviewed_at)}`}
                      </span>
                    </span>
                    <span className={`fy-chip ${chips[level].chip}`}>
                      <Icon of={chips[level].icon} />
                      {words[level]}
                    </span>
                    {officer && (
                      <span className="fy-row gap-2">
                        <Button size="sm" variant="ghost" onClick={() => setEditing(r)} aria-label={`Change ${r.ref}`}>
                          <Icon of={Pencil} />
                          Change
                        </Button>
                        {r.state === "open" && (
                          <Button size="sm" variant="secondary" onClick={() => setReviewing(r)} aria-label={`Review ${r.ref}`} title={helpText("risk.review").tooltip}>
                            <Icon of={ShieldCheck} />
                            Review
                          </Button>
                        )}
                      </span>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </section>
      )}
      {editing && <RiskForm risk={editing === "new" ? null : editing} onClose={() => setEditing(null)} />}
      {reviewing && <ReviewForm risk={reviewing} onClose={() => setReviewing(null)} />}
    </div>
  );
}

// RiskForm adds a risk, or saves a risk's next version (CMP-06).
function RiskForm({ risk, onClose }: { risk: Risk | null; onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [form, setForm] = useState<RiskInput>(
    risk
      ? { title: risk.title, description: risk.description ?? "", owner: risk.owner, likelihood: risk.likelihood, impact: risk.impact, mitigation: risk.mitigation,
          review_months: risk.review_months, next_review_on: risk.next_review_on, state: risk.state }
      : { title: "", description: "", owner: "owner", likelihood: "medium", impact: "medium", mitigation: "", review_months: 3 },
  );
  const save = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () =>
        risk
          ? api.PUT("/v1/compliance/risks/{risk_id}", { params: { path: { risk_id: risk.id }, header: { "Idempotency-Key": newKey(), "If-Match": `"v${risk.version}"` } }, body: form })
          : api.POST("/v1/compliance/risks", { params: { header: { "Idempotency-Key": newKey() } }, body: form }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await queries.invalidateQueries({ queryKey: ["risks"] });
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setProblem(null);
    save.mutate();
  };
  const field = (name: keyof RiskInput, label: string, input: React.ReactNode) => (
    <div className="fy-field">
      <label className="fy-label" htmlFor={`${id}-${name}`}>
        {label}
      </label>
      {input}
    </div>
  );
  const select = (name: "owner" | "likelihood" | "impact" | "state", options: Record<string, string>) => (
    <select className="fy-select" id={`${id}-${name}`} value={form[name] ?? ""} onChange={(e) => setForm({ ...form, [name]: e.target.value })}>
      {Object.entries(options).map(([k, v]) => (
        <option key={k} value={k}>
          {v}
        </option>
      ))}
    </select>
  );
  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()} dismissible={!save.isPending} eyebrow="Risk register"
      title={risk ? `Change ${risk.ref}` : "Add a risk"} description={risk ? "Saves its next version; the earlier ones are kept." : "Its first review is the end of the next quarter unless you choose a day."}>
      {problem && <Refusal problem={problem} title="The risk was not saved" />}
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        {field("title", "Risk", <input className="fy-input" id={`${id}-title`} maxLength={300} value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} />)}
        {field("description", "More about it (optional)", <textarea className="fy-textarea" id={`${id}-description`} maxLength={4000} value={form.description ?? ""} onChange={(e) => setForm({ ...form, description: e.target.value })} />)}
        {field("owner", "Owner", select("owner", owners))}
        {field("likelihood", "Likelihood", select("likelihood", words))}
        {field("impact", "Impact", select("impact", words))}
        {field("mitigation", "Mitigation", <textarea className="fy-textarea" id={`${id}-mitigation`} maxLength={4000} value={form.mitigation} onChange={(e) => setForm({ ...form, mitigation: e.target.value })} />)}
        {field("review_months", "Review every (months)", <input className="fy-input" id={`${id}-review_months`} type="number" min={1} max={12} value={form.review_months ?? 3} onChange={(e) => setForm({ ...form, review_months: Number(e.target.value) })} />)}
        {field("next_review_on", "Next review (optional)", <input className="fy-input" id={`${id}-next_review_on`} type="date" value={form.next_review_on ?? ""} onChange={(e) => setForm({ ...form, next_review_on: e.target.value || undefined })} />)}
        {risk && field("state", "State", select("state", { open: "Open", closed: "Closed" }))}
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={save.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={save.isPending}>
            {risk ? "Save the change" : "Add the risk"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

// ReviewForm records a review of a risk's latest version, which sets its next review date (CMP-06).
function ReviewForm({ risk, onClose }: { risk: Risk; onClose: () => void }) {
  const id = useId();
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [note, setNote] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const review = useMutation({
    mutationFn: async () => {
      const out = await guarded(confirm, () =>
        api.POST("/v1/compliance/risks/{risk_id}/reviews", {
          params: { path: { risk_id: risk.id }, header: { "Idempotency-Key": newKey(), "If-Match": `"v${risk.version}"` } },
          body: { note },
        }),
      );
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async () => {
      await queries.invalidateQueries({ queryKey: ["risks"] });
      onClose();
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  return (
    <Dialog open onOpenChange={(o) => !o && !review.isPending && onClose()} dismissible={!review.isPending} eyebrow="Risk register" title={`Review ${risk.ref}`}
      description={`${risk.title}. Its next review falls ${risk.review_months} months after today.`}>
      {problem && <Refusal problem={problem} title="The review was not recorded" />}
      <form
        className="fy-stack fy-stack--3"
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          setProblem(null);
          review.mutate();
        }}
      >
        <div className="fy-field">
          <label className="fy-label" htmlFor={`${id}-note`}>
            Note (optional)
          </label>
          <textarea className="fy-textarea" id={`${id}-note`} maxLength={2000} value={note} onChange={(e) => setNote(e.target.value)} />
        </div>
        <div className="fy-row justify-end gap-2">
          <Button variant="ghost" type="button" onClick={onClose} disabled={review.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={review.isPending}>
            Record the review
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
