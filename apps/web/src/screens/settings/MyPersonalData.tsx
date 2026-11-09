import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ClockAlert, Send } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Request = components["schemas"]["PDRequestView"];

async function myRequests() {
  const { data, error } = await api.GET("/v1/me/personal-data-requests");
  if (error) throw error;
  return data.items;
}

const statuses: Record<Request["status"], string> = { received: "Received", acknowledged: "Acknowledged", extended: "Extended", answered: "Answered", closed: "Closed" };

// MyPersonalData is the "My personal data" section of Settings › Profile (CMP-PD-04, rev 0.244): a member asks to see,
// or to correct, the personal data the company holds about them, and follows their requests.
export function MyPersonalData() {
  const id = useId();
  const h = helpText("my-data.ask");
  const queries = useQueryClient();
  const list = useQuery({ queryKey: ["my-pd-requests"], queryFn: myRequests });
  const [kind, setKind] = useState<Request["kind"]>("access");
  const [details, setDetails] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [sent, setSent] = useState("");
  const ask = useMutation({
    mutationFn: async () => {
      const out = await api.POST("/v1/me/personal-data-requests", { params: { header: { "Idempotency-Key": newKey() } }, body: { kind, details: details.trim() } });
      if (out.error) throw out.error;
      return out.data;
    },
    onSuccess: async (r) => {
      setDetails("");
      setProblem(null);
      setSent(`Your request is with the Data Protection Officer; it will be answered by ${date(r.due_on)}.`);
      await queries.invalidateQueries({ queryKey: ["my-pd-requests"] });
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    ask.mutate();
  };
  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-title`}>
          My personal data
        </h2>
      </div>
      <div className="fy-card__body fy-stack fy-stack--3">
        <p className="fy-small fy-secondary">{h.tooltip}</p>
        {problem && <Refusal problem={problem} title="The request was not sent" />}
        <p className="fy-small" role="status">
          {sent}
        </p>
        <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
          <fieldset className="fy-field">
            <legend className="fy-label">I want to</legend>
            <label className="fy-check">
              <input type="radio" name={`${id}-kind`} checked={kind === "access"} onChange={() => setKind("access")} />
              See the personal data the company holds about me
            </label>
            <label className="fy-check">
              <input type="radio" name={`${id}-kind`} checked={kind === "correction"} onChange={() => setKind("correction")} />
              Correct it
            </label>
          </fieldset>
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-details`}>
              {kind === "access" ? "What you would like to see" : "What is wrong, and what it should say"}
            </label>
            <textarea className="fy-textarea" id={`${id}-details`} value={details} onChange={(e) => setDetails(e.target.value)} />
          </div>
          <div className="fy-row">
            <Button variant="primary" type="submit" disabled={ask.isPending || details.trim() === ""} title={h.tooltip}>
              <Icon of={Send} />
              {h.label}
            </Button>
          </div>
        </form>
        {list.data && list.data.length > 0 && (
          <ul className="fy-list" aria-label="My requests">
            {list.data.map((r) => (
              <li key={r.id} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">
                    {r.kind === "access" ? "To see my data" : "To correct my data"}: {r.details}
                  </span>
                  <span className="fy-list__meta">
                    Asked {date(r.received_on)} · {statuses[r.status]} · {r.status === "answered" || r.status === "closed" ? "answered" : `due by ${date(r.due_on)}`}
                  </span>
                </span>
                {r.overdue && (
                  <span className="fy-chip fy-chip--serious">
                    <Icon of={ClockAlert} />
                    Overdue
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  );
}
