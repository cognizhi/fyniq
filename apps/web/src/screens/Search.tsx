import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { BookOpen, FileText, Package, Search as SearchIcon, Users } from "lucide-react";
import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { Refusal } from "../components/Refusal";
import { formatAmount } from "../format/amount";
import { date } from "../format/date";
import { helpText } from "../i18n";
import { Icon, problemOf } from "../ui";

type Result = components["schemas"]["SearchResult"];
type Group = components["schemas"]["SearchGroupCount"];

const icons = { contact: Users, account: BookOpen, item: Package } as const;

// moveFocus moves the focus between the results' links with the arrow keys, Home and End (FR-UX-02's keyboard
// navigation); Enter opens the one in focus, as a link does.
function moveFocus(e: KeyboardEvent<HTMLUListElement>) {
  const links = Array.from(e.currentTarget.querySelectorAll<HTMLAnchorElement>("a"));
  const at = links.indexOf(document.activeElement as HTMLAnchorElement);
  const to = { ArrowDown: at + 1, ArrowUp: at - 1, Home: 0, End: links.length - 1 }[e.key];
  if (to === undefined || links.length === 0) return;
  e.preventDefault();
  links[Math.max(0, Math.min(links.length - 1, to))]?.focus();
}

// Search is the global search (docs/design/screens Search; FR-UX-02, SEC-34; PRD-001 rev 0.190): documents, contacts,
// accounts and items found by text or amount, filtered by kind, each kind only where the person's role sees it.
export function Search() {
  const navigate = useNavigate();
  const params = useSearch({ strict: false }) as { q?: string; group?: string };
  const q = (params.q ?? "").trim();
  const group = params.group ?? "";
  const [text, setText] = useState(q);
  useEffect(() => setText(q), [q]);
  const id = useId();
  const field = useRef<HTMLInputElement>(null);
  const h = helpText("shell.search");
  const found = useQuery({
    queryKey: ["search", q, group],
    enabled: q !== "",
    queryFn: async () => {
      const { data, error } = await api.GET("/v1/search", { params: { query: { q, group: (group || undefined) as Group["key"] | undefined } } });
      if (error) throw error;
      return data;
    },
  });
  const go = (next: { q: string; group?: string }) => void navigate({ to: "/search", search: next.group ? next : { q: next.q } });
  const results = found.data?.results ?? [];
  const groups = found.data?.groups ?? [];
  const shown = groups.filter((g) => g.count > 0 || g.key === group);

  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">{h.label}</h1>
          <p className="fy-secondary">{h.tooltip}</p>
        </div>
      </div>
      <form
        role="search"
        className="fy-row gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (text.trim()) go({ q: text.trim() });
        }}
      >
        <label className="fy-sr" htmlFor={`${id}-q`}>
          Find
        </label>
        <input
          ref={field}
          id={`${id}-q`}
          className="fy-input grow"
          type="search"
          maxLength={200}
          value={text}
          placeholder="INV-0138, Acme, 1,234.56 or > 5k"
          onChange={(e) => setText(e.target.value)}
        />
        <button type="submit" className="fy-btn fy-btn--primary">
          <Icon of={SearchIcon} />
          Search
        </button>
      </form>
      {q !== "" && found.data && (
        <div className="fy-row fy-row--wrap gap-2" role="group" aria-label="Kinds of record">
          <button type="button" className="fy-chip" aria-pressed={group === ""} onClick={() => go({ q })}>
            All
          </button>
          {shown.map((g) => (
            <button key={g.key} type="button" className="fy-chip" aria-pressed={group === g.key} onClick={() => go({ q, group: g.key })}>
              {g.label} <span className="fy-num">{g.more ? `${g.count}+` : g.count}</span>
            </button>
          ))}
        </div>
      )}
      {found.isError && <Refusal problem={problemOf(found.error)} title="The search did not run" />}
      {q === "" && <p className="fy-secondary">Type a reference, a name, words of a note or an amount, then select Search.</p>}
      {found.isPending && q !== "" && <p className="fy-secondary">Searching…</p>}
      {found.data && results.length === 0 && (
        <p className="fy-secondary" role="status">
          Nothing matches “{q}” in what you may see. Try fewer words, part of a reference, or an amount.
        </p>
      )}
      {groups.some((g) => g.more) && (
        <p className="fy-small fy-muted" role="status">
          Some kinds found more than are shown: add a word or narrow the amount.
        </p>
      )}
      {results.length > 0 && (
        <ul className="fy-list fy-card" aria-label="Search results" onKeyDown={moveFocus}>
          {results.map((r: Result) => (
            <li key={`${r.group}-${r.id}`} className="fy-list__item">
              <span className="fy-list__icon">
                <Icon of={icons[r.type as keyof typeof icons] ?? FileText} size={20} />
              </span>
              <span className="fy-list__main">
                <span className="fy-list__title whitespace-normal">
                  {r.link ? <Link to={r.link}>{r.title}</Link> : r.title}
                </span>
                <span className="fy-list__meta whitespace-normal">
                  {[groups.find((g) => g.key === r.group)?.label, r.date ? date(r.date) : "", r.status, r.detail].filter(Boolean).join(" · ")}
                </span>
              </span>
              {r.amount && (
                <span className="fy-num">
                  {formatAmount(r.amount.amount)} {r.amount.currency}
                </span>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
