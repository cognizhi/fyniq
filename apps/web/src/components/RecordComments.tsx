import { useQuery, useQueryClient } from "@tanstack/react-query";
import { MessageSquare, Send } from "lucide-react";
import { useId, useRef, useState, type ChangeEvent, type FormEvent, type KeyboardEvent } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { dateTime } from "../format/date";
import { useStatusPhrase } from "../shell/status";
import { Icon, problemOf, type ApiProblem } from "../ui";
import { Refusal } from "./Refusal";
import { Button } from "./ui/button";

// Comments and @mentions on a record (PRD-001 FR-UX-13; experience/navigation "Activity and Related tabs"): the thread, oldest
// first, and a box to add to it. Typing "@" and the start of a name offers the members who may see this record, and only them: the
// list comes from the API, which never offers a name for a record its owner may not open. A mention tells that member, and only
// them. A comment is never changed or removed: answer a mistake with another comment.

export type CommentRecordType = "invoice" | "credit_note" | "bill" | "vendor_credit" | "journal" | "expense_claim" | "quote" | "sales_order" | "purchase_order" | "contact";
type Mentionable = components["schemas"]["Mentionable"];
type Comment = components["schemas"]["RecordComment"];

const MAX = 2000;

// The "@word" being typed just before the caret, if any.
function typing(text: string, caret: number): { start: number; query: string } | null {
  const before = text.slice(0, caret);
  const at = before.lastIndexOf("@");
  if (at < 0 || (at > 0 && !/\s/.test(before[at - 1] ?? ""))) return null;
  const query = before.slice(at + 1);
  return /^[^\n@]{0,40}$/.test(query) ? { start: at, query } : null;
}

export function RecordComments({ recordType, recordId }: { recordType: CommentRecordType; recordId: string }) {
  const queries = useQueryClient();
  const id = useId();
  const box = useRef<HTMLTextAreaElement>(null);
  const [text, setText] = useState("");
  const [mentions, setMentions] = useState<Mentionable[]>([]);
  const [word, setWord] = useState<{ start: number; query: string } | null>(null);
  const [active, setActive] = useState(0);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [added, setAdded] = useState<string | null>(null);
  const path = { record_type: recordType, record_id: recordId };

  const thread = useQuery({
    queryKey: ["comments", recordType, recordId],
    queryFn: async () => {
      const items: Comment[] = [];
      let cursor: string | undefined;
      for (let page = 0; page < 20; page++) {
        const { data, error } = await api.GET("/v1/records/{record_type}/{record_id}/comments", { params: { path, query: { page_size: 100, cursor } } });
        if (error) throw error;
        items.push(...(data.items ?? []));
        cursor = data.next_cursor || undefined;
        if (!cursor) break;
      }
      return items;
    },
  });
  const q = word?.query.trim() ?? "";
  const names = useQuery({
    queryKey: ["mentionable", recordType, recordId, q],
    enabled: word !== null,
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/records/{record_type}/{record_id}/mentionable", { params: { path, query: { q: q || undefined, page_size: 8 } } });
      if (error) throw error;
      return data.items ?? [];
    },
  });
  const options = word ? (names.data ?? []) : [];
  const status = useStatusPhrase(word ? (options.length === 0 ? "No member to mention" : `${options.length} to mention`) : null);

  const edit = (e: ChangeEvent<HTMLTextAreaElement>) => {
    setText(e.target.value);
    setWord(typing(e.target.value, e.target.selectionStart));
    setActive(0);
  };
  const choose = (m: Mentionable) => {
    if (!word) return;
    const next = `${text.slice(0, word.start)}@${m.name} ${text.slice(box.current?.selectionStart ?? word.start + word.query.length + 1)}`;
    setText(next);
    setMentions((list) => (list.some((x) => x.id === m.id) ? list : [...list, m]));
    setWord(null);
    const caret = word.start + m.name.length + 2;
    requestAnimationFrame(() => {
      box.current?.focus();
      box.current?.setSelectionRange(caret, caret);
    });
  };
  const onKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (!word || options.length === 0) return;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      setActive((i) => (i + (e.key === "ArrowDown" ? 1 : options.length - 1)) % options.length);
    } else if (e.key === "Enter" || e.key === "Tab") {
      e.preventDefault();
      choose(options[Math.min(active, options.length - 1)]!);
    } else if (e.key === "Escape") {
      e.preventDefault();
      setWord(null);
    }
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!text.trim()) return;
    setBusy(true);
    setProblem(null);
    // Only those still named in the text are mentioned.
    const mention_ids = mentions.filter((m) => text.includes(`@${m.name}`)).map((m) => m.id);
    const out = await api.POST("/v1/records/{record_type}/{record_id}/comments", {
      params: { path, header: { "Idempotency-Key": crypto.randomUUID() } },
      body: { body: text, ...(mention_ids.length ? { mention_ids } : {}) },
    });
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    setText("");
    setMentions([]);
    setWord(null);
    setAdded(mention_ids.length ? `Comment added; ${mention_ids.length === 1 ? "1 person was" : `${mention_ids.length} people were`} told` : "Comment added");
    await queries.invalidateQueries({ queryKey: ["comments", recordType, recordId] });
  };

  const items = thread.data ?? [];
  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-title`}>
          Comments
        </h2>
        <span className="fy-badge fy-badge--quiet">{items.length}</span>
      </div>
      <div className="fy-card__body fy-stack fy-stack--4">
        {thread.isError && <Refusal problem={problemOf(thread.error)} title="The comments could not be read" />}
        {thread.isPending && <p className="fy-secondary">Loading the comments…</p>}
        {thread.data && items.length === 0 && (
          <p className="fy-secondary">
            No comments yet. Write one to leave a note on this record; type @ and a name to tell a colleague who may see it.
          </p>
        )}
        {items.length > 0 && (
          <ol className="fy-list" aria-label="Comments">
            {items.map((c) => (
              <li key={c.id} className="fy-list__item">
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">
                    {c.author.name || "A former member"}
                    <span className="fy-small fy-secondary"> · {dateTime(c.created_at)}</span>
                  </span>
                  <span className="whitespace-pre-wrap">{c.body}</span>
                </span>
              </li>
            ))}
          </ol>
        )}
        <form className="fy-stack fy-stack--2" onSubmit={(e) => void submit(e)} noValidate>
          {problem && <Refusal problem={problem} title="The comment was not added" />}
          <div className="fy-field">
            <label className="fy-label" htmlFor={`${id}-box`}>
              Add a comment
            </label>
            <div className="relative">
              <textarea
                ref={box}
                id={`${id}-box`}
                className="fy-textarea"
                rows={3}
                maxLength={MAX}
                value={text}
                onChange={edit}
                onKeyDown={onKey}
                onBlur={() => setTimeout(() => setWord(null), 150)}
                aria-controls={word !== null && options.length > 0 ? `${id}-list` : undefined}
                aria-autocomplete="list"
                aria-activedescendant={word && options.length > 0 ? `${id}-opt-${Math.min(active, options.length - 1)}` : undefined}
                aria-describedby={`${id}-help`}
              />
              {word !== null && options.length > 0 && (
                <ul className="fy-card absolute z-10 mt-1 w-full max-w-sm p-1" id={`${id}-list`} role="listbox" aria-label="Members to mention">
                  {options.map((m, i) => (
                    <li key={m.id} id={`${id}-opt-${i}`} role="option" aria-selected={i === Math.min(active, options.length - 1)} className="cursor-default rounded-sm px-2 py-1.5 aria-selected:bg-surface-3"
                      onMouseDown={(e) => {
                        e.preventDefault();
                        choose(m);
                      }}>
                      {m.name}
                    </li>
                  ))}
                </ul>
              )}
            </div>
            <span className="fy-help" id={`${id}-help`}>
              Type @ and a name to mention a member who may see this record; they are told. A comment stays on the record: answer a mistake with another comment. {MAX - text.length} characters left.
            </span>
          </div>
          {status}
          <div className="fy-row fy-row--wrap justify-end">
            {added && (
              <span className="fy-small fy-secondary" role="status">
                {added}
              </span>
            )}
            <Button variant="primary" type="submit" disabled={busy || !text.trim()}>
              <Icon of={text.trim() ? Send : MessageSquare} />
              {busy ? "Adding…" : "Add comment"}
            </Button>
          </div>
        </form>
      </div>
    </section>
  );
}

// CommentsOnDemand is the comments of a record for a page that is not laid out in the record page anatomy yet: a card that opens
// the thread when it is asked for, so a page that never needs it never reads it.
export function CommentsOnDemand({ recordType, recordId }: { recordType: CommentRecordType; recordId: string }) {
  const id = useId();
  const [open, setOpen] = useState(false);
  if (open) return <RecordComments recordType={recordType} recordId={recordId} />;
  return (
    <section className="fy-card" aria-labelledby={`${id}-title`}>
      <div className="fy-card__head">
        <h2 className="fy-h3" id={`${id}-title`}>
          Comments
        </h2>
        <Button variant="ghost" size="sm" type="button" aria-expanded={false} onClick={() => setOpen(true)} title="Read the comments on this record, or add one, and mention a colleague who may see it">
          <Icon of={MessageSquare} />
          Show comments
        </Button>
      </div>
    </section>
  );
}
