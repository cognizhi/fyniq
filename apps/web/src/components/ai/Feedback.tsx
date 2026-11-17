import { useMutation } from "@tanstack/react-query";
import { ThumbsDown, ThumbsUp } from "lucide-react";
import { type FormEvent, useId, useState } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { helpText } from "../../i18n";
import { type ApiProblem, Icon, problemOf } from "../../ui";
import { Refusal } from "../Refusal";
import { Button } from "../ui/button";

type Rating = components["schemas"]["AITurnRating"];

const stateWords: Record<Rating["state"], string> = {
  waiting: "an Owner or Administrator will review it",
  golden: "it now helps test the assistant",
  dismissed: "it was reviewed",
};

// TurnFeedback is a thumbs up or down on an answer, with a reason, once (QA-AI-05): an Owner or Administrator reviews it,
// with the question and the answer, before it joins the set the assistant is tested with.
export function TurnFeedback({ conversation, turn, rated, onRated }: { conversation: string; turn: string; rated?: Rating; onRated: (r: Rating) => void }) {
  const id = useId();
  const h = helpText("assistant.feedback");
  const [rating, setRating] = useState<"up" | "down" | null>(null);
  const [reason, setReason] = useState("");
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const give = useMutation({
    mutationFn: async () => {
      const { data, error } = await api.POST("/v1/assistant/conversations/{conversation_id}/turns/{turn_id}/feedback", {
        params: { path: { conversation_id: conversation, turn_id: turn }, header: { "Idempotency-Key": newKey() } },
        body: { rating: rating ?? "up", reason: reason.trim() },
      });
      if (error) throw error;
      return data;
    },
    onSuccess: (f) => {
      setProblem(null);
      onRated({ id: f.id, rating: f.rating, reason: f.reason ?? "", state: f.state });
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  if (rated) {
    return (
      <p className="fy-small fy-secondary fy-row gap-1">
        <Icon of={rated.rating === "up" ? ThumbsUp : ThumbsDown} />
        You rated it {rated.rating === "up" ? "helpful" : "not helpful"}: {stateWords[rated.state]}.
      </p>
    );
  }
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (reason.trim()) give.mutate();
  };
  return (
    <div className="fy-stack gap-1">
      <div className="fy-row gap-1" role="group" aria-label={h.label} title={h.tooltip}>
        <button className="fy-iconbtn" type="button" aria-label="Helpful" aria-pressed={rating === "up"} onClick={() => setRating("up")}>
          <Icon of={ThumbsUp} />
        </button>
        <button className="fy-iconbtn" type="button" aria-label="Not helpful" aria-pressed={rating === "down"} onClick={() => setRating("down")}>
          <Icon of={ThumbsDown} />
        </button>
      </div>
      {rating && (
        <form className="fy-stack gap-1" onSubmit={submit}>
          <label className="fy-label" htmlFor={`${id}-reason`}>
            {rating === "up" ? "What made it helpful?" : "What was wrong with it?"}
          </label>
          <textarea id={`${id}-reason`} className="fy-input" rows={2} maxLength={1000} required value={reason} onChange={(e) => setReason(e.target.value)}
            aria-describedby={`${id}-who`} />
          <span className="fy-help" id={`${id}-who`}>
            An Owner or Administrator reads it with your question and the answer.
          </span>
          {problem && <Refusal problem={problem} title="Your rating was not kept" />}
          <span className="fy-row gap-1">
            <Button size="sm" variant="secondary" type="submit" disabled={give.isPending || reason.trim() === ""}>
              Send the rating
            </Button>
            <Button size="sm" variant="ghost" type="button" onClick={() => setRating(null)}>
              Cancel
            </Button>
          </span>
        </form>
      )}
    </div>
  );
}
