import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { BookOpen, FileText, Landmark, RotateCcw, Send, Sparkles, SquarePen, Trash2, X } from "lucide-react";
import { type FormEvent, type KeyboardEvent, useEffect, useId, useRef, useState } from "react";
import { api } from "../../api/client";
import { date } from "../../format/date";
import type { Mode } from "../../registry";
import { type ApiProblem, Icon, problemOf } from "../../ui";
import { guideLink } from "../../help/links";
import { Refusal } from "../Refusal";
import { Button } from "../ui/button";
import { AiBadge } from "./AiBadge";
import { AnswerText } from "./AnswerText";
import { ask, type Fact, routeOf, startConversation, type Turn, type TurnContext } from "./assistant";
import { TurnFeedback } from "./Feedback";
import { CashSimulate } from "./Simulate";

// The codes the panel answers in its own words (the rest are shown as the API words them).
const switchedOff = "AI-ROUTER-05";
const budgetSpent = "AI-BUDGET-02";
const conversationFull = "AI-LIMIT-03";

const suggestions = ["What was our revenue last month?", "How much cash do we have?", "Which checks of the books need attention?"];

type Lane = "ask" | "explain" | "simulate";
const lanes: [Lane, string][] = [
  ["ask", "Ask"],
  ["explain", "Explain"],
  ["simulate", "Simulate"],
];

// AssistantPanel is the AI assistant panel (screen ai-assistant-panel; PRD-001 FR-UX-17, AI-08, AI-22, AI-68, AI-69;
// p1-ai-assistant design D12): a person's own conversations with the assistant, asked through the API, each answer
// streamed while it reads the books and kept. Every AI message is violet with the AI badge; every figure is rendered
// from its fact, with a citation chip that opens the report, ledger or record it came from, and Expert mode shows each
// fact's source and query. Full screen in compact, a sheet in medium, docked beside the page in expanded (the kit's
// .fy-ai). It shows the AI switched off, the budget spent and a full conversation in words, and never stores an answer
// it did not get from the API (FR-UX-22). Its lanes: Ask in your words; Explain what is asked about, which the assistant
// reads through lineage.explain or the record; Simulate the invoice or bill asked about through the cash what-if (task
// 7.2). Each answer takes a thumbs up or down with a reason (QA-AI-05). Prepare waits for p1-ai-proposals.
export function AssistantPanel({ enabled, mode, context, onClearContext, onClose }: {
  enabled: boolean;
  mode: Mode;
  context?: TurnContext;
  onClearContext: () => void;
  onClose: () => void;
}) {
  const id = useId();
  const queries = useQueryClient();
  const input = useRef<HTMLTextAreaElement>(null);
  const [view, setView] = useState<"thread" | "history">("thread");
  const [conversation, setConversation] = useState<string | null>(null);
  const [turns, setTurns] = useState<Turn[]>([]);
  const [pending, setPending] = useState<{ question: string; status: string } | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [question, setQuestion] = useState("");
  const [said, setSaid] = useState("");
  const [lane, setLane] = useState<Lane>("ask");
  const expert = mode === "expert";

  useEffect(() => input.current?.focus(), []);

  const history = useQuery({
    queryKey: ["ai-conversations"],
    enabled: view === "history",
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/assistant/conversations", { params: { query: { page_size: 50 } } });
      if (error) throw error;
      return data.items;
    },
  });
  const open = useMutation({
    mutationFn: async (c: string) => {
      const { data, error } = await api.GET("/v1/assistant/conversations/{conversation_id}", { params: { path: { conversation_id: c } } });
      if (error) throw error;
      return data;
    },
    onSuccess: (c) => {
      setConversation(c.id);
      setTurns(c.turns ?? []);
      setProblem(null);
      setView("thread");
    },
    onError: (e) => setProblem(problemOf(e)),
  });
  const remove = useMutation({
    mutationFn: async (c: string) => {
      const { error } = await api.DELETE("/v1/assistant/conversations/{conversation_id}", { params: { path: { conversation_id: c } } });
      if (error) throw error;
      return c;
    },
    onSuccess: async (c) => {
      if (c === conversation) fresh();
      await queries.invalidateQueries({ queryKey: ["ai-conversations"] });
    },
    onError: (e) => setProblem(problemOf(e)),
  });

  const fresh = () => {
    setConversation(null);
    setTurns([]);
    setProblem(null);
    setView("thread");
    input.current?.focus();
  };

  const send = async (words: string) => {
    const q = words.trim();
    if (!q || pending) return;
    setQuestion("");
    setProblem(null);
    setSaid("");
    setPending({ question: q, status: "Reading the books" });
    try {
      let c = conversation;
      if (!c) {
        c = (await startConversation(q.slice(0, 200))).id;
        setConversation(c);
      }
      await ask(c, q, context, (e) => {
        if (e.kind === "status") setPending({ question: q, status: e.status.tool ? `${e.status.text} (${e.status.tool})` : e.status.text });
        if (e.kind === "answer") {
          setTurns((t) => [...t, e.turn]);
          setSaid("The assistant answered.");
        }
        if (e.kind === "error") setProblem(problemOf(e.problem));
      });
    } catch (e) {
      setProblem(problemOf(e));
    } finally {
      setPending(null);
      void queries.invalidateQueries({ queryKey: ["ai-conversations"] });
    }
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    void send(question);
  };
  const keys = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      void send(question);
    }
  };

  const off = !enabled || problem?.code === switchedOff;
  return (
    <aside className="fy-ai" aria-labelledby={`${id}-title`} onKeyDown={(e) => e.key === "Escape" && onClose()}>
      <div className="fy-ai__head">
        <Icon of={Sparkles} size={20} />
        <h2 className="fy-h3 fy-grow" id={`${id}-title`} style={{ color: "inherit" }}>
          Assistant
        </h2>
        <button className="fy-iconbtn" type="button" aria-label="Your conversations" title="Your conversations" aria-pressed={view === "history"}
          onClick={() => setView(view === "history" ? "thread" : "history")}>
          <Icon of={RotateCcw} size={20} />
        </button>
        <button className="fy-iconbtn" type="button" aria-label="New conversation" title="New conversation" onClick={fresh}>
          <Icon of={SquarePen} size={20} />
        </button>
        <button className="fy-iconbtn" type="button" aria-label="Close the assistant" title="Close the assistant (Ctrl+J)" onClick={onClose}>
          <Icon of={X} size={20} />
        </button>
      </div>
      {context && (
        <div className="fy-ai__context">
          <Icon of={FileText} />
          <span className="fy-grow fy-truncate">
            Context: <span className="fy-strong">{context.label}</span>
          </span>
          <Button variant="ghost" size="sm" onClick={onClearContext} aria-label="Remove the context">
            Remove
          </Button>
        </div>
      )}

      {view === "thread" && !off && (
        <div className="px-4 pt-3">
          <div className="fy-segmented w-full" role="radiogroup" aria-label="What do you want to do">
            {lanes.map(([l, label], i) => (
              <button key={l} type="button" role="radio" className="flex-1" aria-checked={lane === l} tabIndex={lane === l ? 0 : -1} data-lane={l}
                onClick={() => setLane(l)}
                onKeyDown={(e) => {
                  const step = e.key === "ArrowRight" || e.key === "ArrowDown" ? 1 : e.key === "ArrowLeft" || e.key === "ArrowUp" ? -1 : 0;
                  if (!step) return;
                  e.preventDefault();
                  const next = lanes[(i + step + lanes.length) % lanes.length]?.[0] ?? "ask";
                  setLane(next);
                  (e.currentTarget.parentElement?.querySelector(`[data-lane="${next}"]`) as HTMLElement | null)?.focus();
                }}>
                {label}
              </button>
            ))}
          </div>
        </div>
      )}
      {view === "history" ? (
        <div className="fy-ai__thread">
          <h3 className="fy-h3">Your conversations</h3>
          {history.isError && <Refusal problem={problemOf(history.error)} title="Your conversations could not be shown" />}
          {history.data && history.data.length === 0 && <p className="fy-secondary">No conversations yet.</p>}
          <ul className="fy-stack gap-2">
            {(history.data ?? []).map((c) => (
              <li key={c.id} className="fy-row gap-2">
                <button type="button" className="fy-grow fy-stack gap-0 text-left bg-transparent border-0 p-0 font-[inherit] cursor-pointer" onClick={() => open.mutate(c.id)}>
                  <span className="fy-strong fy-truncate">{c.title || "Untitled conversation"}</span>
                  <span className="fy-small fy-secondary">{date(c.created_at)}</span>
                </button>
                <ConfirmDelete label={c.title || "Untitled conversation"} onDelete={() => remove.mutate(c.id)} />
              </li>
            ))}
          </ul>
        </div>
      ) : (
        <div className="fy-ai__thread">
          {off && (
            <section className="fy-card fy-card--flat" aria-label="The assistant is switched off">
              <div className="fy-card__body fy-stack gap-2">
                <p className="fy-strong">The assistant is switched off</p>
                <p className="fy-small">The organisation's Owner switched the AI off, or has not switched it on. Nothing you ask is sent anywhere.</p>
                <Link to="/settings/ai" className="fy-small">
                  AI settings
                </Link>
              </div>
            </section>
          )}
          {!off && turns.length === 0 && !pending && (
            <div className="fy-stack gap-2">
              <p className="fy-secondary">Ask about your books in your own words. Every figure comes from Fyniq, with where it came from.</p>
              <div className="fy-stack gap-1" role="group" aria-label="Suggested questions">
                {suggestions.map((s) => (
                  <Button key={s} variant="secondary" size="sm" className="self-start" onClick={() => void send(s)}>
                    {s}
                  </Button>
                ))}
              </div>
            </div>
          )}
          {lane === "simulate" && !off ? (
            <CashSimulate context={context} />
          ) : (
            <>
              {turns.map((t) => (
                <TurnView key={t.id} turn={t} expert={expert} conversation={conversation ?? ""}
                  onRated={(f) => setTurns((all) => all.map((x) => (x.id === t.id ? { ...x, feedback: f } : x)))} />
              ))}
              {pending && (
                <>
                  <div className="fy-msg fy-msg--user">
                    <p>{pending.question}</p>
                  </div>
                  <div className="fy-msg fy-msg--ai">
                    <AiBadge />
                    <p className="fy-ai-text fy-small fy-secondary">{pending.status}…</p>
                  </div>
            </>
          )}
          {problem && !off && <TurnProblem problem={problem} onNew={fresh} />}
            </>
          )}
        </div>
      )}
      <p className="fy-sr" role="status">
        {pending ? pending.status : said}
      </p>

      {lane === "explain" && !off && view === "thread" ? (
        <div className="fy-ai__composer">
          {context ? (
            <Button variant="secondary" className="self-start" disabled={!!pending}
              onClick={() => void send(`Explain ${context.label}: where it comes from and what it is made of.`)}>
              Explain {context.label}
            </Button>
          ) : (
            <p className="fy-small fy-secondary">Choose Ask about this on a figure, a report or a record to have the assistant explain where it comes from.</p>
          )}
        </div>
      ) : lane === "simulate" && !off && view === "thread" ? null : (
        <form className="fy-ai__composer" onSubmit={submit}>
          <label className="fy-sr" htmlFor={`${id}-ask`}>
            Ask the assistant
          </label>
          <div className="fy-inputgroup">
            <textarea id={`${id}-ask`} ref={input} className="fy-input" rows={2} value={question} disabled={off} maxLength={4000}
              placeholder={context ? `Ask about ${context.label}…` : "Ask about your books…"} onChange={(e) => setQuestion(e.target.value)} onKeyDown={keys} />
            <button className="fy-iconbtn" type="submit" aria-label="Send" title="Send" disabled={off || !!pending || question.trim() === ""}>
              <Icon of={Send} size={20} />
            </button>
          </div>
          <div className="fy-row fy-row--between fy-small fy-muted gap-2">
            <span>Uses only what you can see. It never sees identity or bank account numbers, nor payroll unless an Owner allows it.</span>
            <span className="fy-from-medium whitespace-nowrap">
              <span className="fy-kbd">Ctrl J</span> to close
            </span>
          </div>
        </form>
      )}
    </aside>
  );
}

// TurnView is a question and the assistant's answer, as the API kept it.
function TurnView({ turn: t, expert, conversation, onRated }: {
  turn: Turn;
  expert: boolean;
  conversation: string;
  onRated: (r: NonNullable<Turn["feedback"]>) => void;
}) {
  return (
    <>
      <div className="fy-msg fy-msg--user">
        <p>{t.question}</p>
      </div>
      <div className="fy-msg fy-msg--ai">
        <AiBadge />
        {t.status === "answered" ? (
          <AnswerText segments={t.segments} />
        ) : (
          <p className="fy-ai-text">
            {t.status === "refused" ? "The assistant did not answer this." : "The assistant could not finish this answer."} {t.code && <span className="fy-mono fy-small">{t.code}</span>}
          </p>
        )}
        {t.withheld > 0 && (
          <p className="fy-small fy-secondary">
            Fyniq withheld {t.withheld === 1 ? "a figure" : `${t.withheld} figures`} it could not check against the books.
          </p>
        )}
        {t.sources.length > 0 && (
          <div className="fy-row fy-row--wrap gap-1" aria-label="Where the explanation comes from" role="group">
            {t.sources.map((s) => (
              <GuideSource key={`${s.guide}#${s.anchor}`} source={s} />
            ))}
          </div>
        )}
        {t.cited.length > 0 && (
          <div className="fy-row fy-row--wrap gap-1" aria-label="Where the figures come from" role="group">
            {t.cited.map((f) => (
              <Citation key={f.id} fact={f} />
            ))}
          </div>
        )}
        {expert &&
          t.cited.map((f) => (
            <p key={f.id} className="fy-small fy-mono fy-muted" style={{ overflowWrap: "anywhere" }}>
              {f.measure}
              {f.member_name ? ` (${f.member_name})` : ""} · {f.value ?? "no value"} · {f.lineage} · ledger revision {f.ledger_revision}
            </p>
          ))}
        {conversation && (t.status === "answered" || t.status === "refused") && (
          <TurnFeedback conversation={conversation} turn={t.id} rated={t.feedback} onRated={onRated} />
        )}
      </div>
    </>
  );
}

// GuideSource is a chip naming the guide page an explanation comes from, opening it (AI-51): always a page the person can open
// in the user guide, never one of the assistant's own compiled pages. A page still awaiting review says so.
function GuideSource({ source: s }: { source: NonNullable<Turn["sources"]>[number] }) {
  const { to, hash } = guideLink(`${s.guide}#${s.anchor}`);
  return (
    <Link className="fy-tag" to={to} hash={hash} title={s.draft ? "This guide page is still awaiting review" : `From the guide: ${s.title}`}>
      <Icon of={BookOpen} />
      {s.title}
      {s.draft ? " (awaiting review)" : ""}
    </Link>
  );
}

// Citation is a chip naming a fact's source, opening the page it came from when the web app has one (AI-68).
function Citation({ fact: f }: { fact: Fact }) {
  const period = f.from && f.to ? (f.from === f.to ? `, ${date(f.from)}` : `, ${date(f.from)} to ${date(f.to)}`) : "";
  const label = `${f.name || f.measure}${f.member_name ? `, ${f.member_name}` : ""}${period}`;
  const to = routeOf(f.lineage);
  const body = (
    <>
      <Icon of={Landmark} />
      {label}
    </>
  );
  return to ? (
    <Link className="fy-tag" to={to} title={`From ${f.source}`}>
      {body}
    </Link>
  ) : (
    <span className="fy-tag" title={`From ${f.source}`}>
      {body}
    </span>
  );
}

// TurnProblem says what stopped a question, in words, with what to do.
function TurnProblem({ problem, onNew }: { problem: ApiProblem; onNew: () => void }) {
  if (problem.code === budgetSpent) {
    return (
      <section className="fy-card fy-card--flat" aria-label="The AI budget is spent">
        <div className="fy-card__body fy-stack gap-1">
          <p className="fy-strong">The AI budget is spent for this month</p>
          <p className="fy-small">{problem.detail ?? "The organisation's AI budget is used up, so the question was not sent."}</p>
          <p className="fy-small fy-secondary">{problem.fix ?? "An Owner can raise the budget in AI settings."}</p>
        </div>
      </section>
    );
  }
  return (
    <div className="fy-stack gap-2">
      <Refusal problem={problem} title="The assistant did not answer" />
      {problem.code === conversationFull && (
        <Button variant="secondary" size="sm" className="self-start" onClick={onNew}>
          Start a new conversation
        </Button>
      )}
    </div>
  );
}

// ConfirmDelete asks once before a conversation is deleted.
function ConfirmDelete({ label, onDelete }: { label: string; onDelete: () => void }) {
  const [asking, setAsking] = useState(false);
  if (!asking) {
    return (
      <button className="fy-iconbtn" type="button" aria-label={`Delete ${label}`} title="Delete" onClick={() => setAsking(true)}>
        <Icon of={Trash2} />
      </button>
    );
  }
  return (
    <span className="fy-row gap-1">
      <Button size="sm" variant="secondary" onClick={onDelete} aria-label={`Delete ${label} for good`}>
        Delete
      </Button>
      <Button size="sm" variant="ghost" onClick={() => setAsking(false)}>
        Keep
      </Button>
    </span>
  );
}

export default AssistantPanel;
