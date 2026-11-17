import { useMutation, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { type FormEvent, useId, useState } from "react";
import { api } from "../../api/client";
import type { components } from "../../api/schema";
import { formatAmount } from "../../format/amount";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { problemOf } from "../../ui";
import { Refusal } from "../Refusal";
import { Button } from "../ui/button";
import type { TurnContext } from "./assistant";

type Projection = components["schemas"]["CashProjection"];

const days = 90;

// CashSimulate is the panel's Simulate (p1-ai-assistant design D12; FR-PLN-08, FR-PLN-09): the invoice or bill being
// asked about paid on another day, or not in the next 90 days, through the cash projection's what-if, which saves
// nothing. The figures are Fyniq's, read from the API, so they carry no AI mark; "Save as proposal" waits for
// p1-ai-proposals.
export function CashSimulate({ context }: { context?: TurnContext }) {
  const id = useId();
  const h = helpText("assistant.simulate");
  const record = context && (context.kind === "invoice" || context.kind === "bill") && context.id ? context : undefined;
  const [choice, setChoice] = useState<"move" | "leave">("move");
  const [on, setOn] = useState("");
  const now = useQuery({
    queryKey: ["cash-projection", days],
    enabled: record !== undefined,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/cash-projection", { params: { query: { days } } });
      if (error) throw error;
      return data;
    },
  });
  const whatIf = useMutation({
    mutationFn: async () => {
      const move = choice === "leave" ? { key: record?.id ?? "", leave_out: true } : { key: record?.id ?? "", to: on };
      const { data, error } = await api.POST("/v1/cash-projection:what-if", { body: { days, moves: [move] } });
      if (error) throw error;
      return data;
    },
  });
  if (!record) {
    return (
      <div className="fy-stack gap-2">
        <p className="fy-small">{h.tooltip}</p>
        <p className="fy-small fy-secondary">
          Open an invoice or a bill and choose <span className="fy-strong">Ask about this</span>, or try more on <Link to="/planning/cash">Cash ahead</Link>.
        </p>
      </div>
    );
  }
  const movement = now.data?.movements.find((m) => m.key === record.id);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    whatIf.mutate();
  };
  return (
    <div className="fy-stack gap-3">
      <p className="fy-small">{h.tooltip}</p>
      {now.isError && <Refusal problem={problemOf(now.error)} title="The cash projection could not be read" />}
      {now.data && !movement && (
        <p className="fy-small fy-secondary">
          {record.label} is not expected to move money in the next {days} days, so there is nothing to simulate. See <Link to="/planning/cash">Cash ahead</Link>.
        </p>
      )}
      {movement && (
        <form className="fy-stack gap-2" onSubmit={submit}>
          <p className="fy-small">
            {record.label}: {formatAmount(movement.amount.amount)} {movement.amount.currency}, expected on {date(movement.expected_on)}.
          </p>
          <fieldset className="fy-stack gap-1">
            <legend className="fy-label">What if</legend>
            <label className="fy-row gap-2">
              <input type="radio" name={`${id}-choice`} checked={choice === "move"} onChange={() => setChoice("move")} />
              it moves on another day
            </label>
            <label className="fy-row gap-2">
              <input type="radio" name={`${id}-choice`} checked={choice === "leave"} onChange={() => setChoice("leave")} />
              it does not move in the next {days} days
            </label>
          </fieldset>
          {choice === "move" && (
            <div className="fy-field">
              <label className="fy-label" htmlFor={`${id}-on`}>
                On
              </label>
              <input id={`${id}-on`} className="fy-input" type="date" required value={on} min={now.data?.today} onChange={(e) => setOn(e.target.value)} />
            </div>
          )}
          {whatIf.isError && <Refusal problem={problemOf(whatIf.error)} title="The what-if could not be worked out" />}
          <Button variant="secondary" size="sm" type="submit" className="self-start" disabled={whatIf.isPending || (choice === "move" && on === "")}>
            {h.label}
          </Button>
        </form>
      )}
      {now.data && whatIf.data && (
        <table className="fy-table" aria-label="The cash projection, now and in the what-if">
          <thead>
            <tr>
              <th scope="col">In the next {days} days</th>
              <th scope="col" className="fy-num">
                Now
              </th>
              <th scope="col" className="fy-num">
                What if
              </th>
            </tr>
          </thead>
          <tbody>
            <Row label={`Lowest balance (${now.data.currency})`} now={now.data} then={whatIf.data} of={(p) => `${formatAmount(p.lowest.amount)}, ${date(p.lowest_on)}`} />
            <Row label="First day below the buffer" now={now.data} then={whatIf.data} of={(p) => (p.below_buffer_on ? date(p.below_buffer_on) : "None")} />
          </tbody>
        </table>
      )}
      {whatIf.data && <p className="fy-help">Nothing was saved or changed: the invoice, the bill and their dates stay as they are.</p>}
    </div>
  );
}

function Row({ label, now, then, of }: { label: string; now: Projection; then: Projection; of: (p: Projection) => string }) {
  return (
    <tr>
      <td>{label}</td>
      <td className="fy-num">{of(now)}</td>
      <td className="fy-num">{of(then)}</td>
    </tr>
  );
}
