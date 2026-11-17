import { Link, useParams } from "@tanstack/react-router";
import { BookOpen, ChevronLeft, Printer, Search as SearchIcon, Sparkle } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { Button } from "../../components/ui/button";
import { forPerson, headings, pageOf, pages, search, whatsNew } from "../../help/guide";
import { Markdown } from "../../help/markdown";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { Icon } from "../../ui";

// Help is the user guide's index at /help (FR-UX-32; screen user-guide): every page of docs/user-guide/ the person's
// roles and mode call for, by title, searched as they type. In Basic mode it says how many pages Expert mode adds.
export function Help() {
  const { session, mode } = useSignedIn();
  const id = useId();
  const h = helpText("help.guide");
  const [q, setQ] = useState("");
  const { shown, hidden } = forPerson(pages, session.roles ?? [], mode);
  const found = search(shown, q);
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1" title={h.tooltip}>
            {h.label}
          </h1>
          <p className="fy-secondary">How to do each thing in Fyniq, and what its words mean. It is the same guide the assistant reads.</p>
        </div>
      </div>
      <div className="fy-field max-w-xl">
        <label className="fy-label" htmlFor={`${id}-q`}>
          Search the guide
        </label>
        <div className="fy-inputgroup">
          <Icon of={SearchIcon} />
          <input id={`${id}-q`} className="fy-input" type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Such as record a bill" />
        </div>
      </div>
      <p role="status" aria-live="polite" className="fy-small fy-secondary">
        {q ? `${found.length} page${found.length === 1 ? "" : "s"} found` : `${shown.length} pages`}
        {mode !== "expert" && hidden > 0 ? `; Expert mode adds ${hidden} more` : ""}
      </p>
      <ul className="fy-list fy-card" aria-label="Pages of the guide">
        {!q && (
          <li>
            <Link className="fy-list__item" to="/help/$key" params={{ key: whatsNewKey }}>
              <span className="fy-list__icon">
                <Icon of={Sparkle} size={20} />
              </span>
              <span className="fy-list__main">
                <span className="fy-list__title">What's new</span>
                <span className="fy-list__meta">The release notes, newest first</span>
              </span>
            </Link>
          </li>
        )}
        {found.map((p) => (
          <li key={p.key}>
            <Link className="fy-list__item" to="/help/$key" params={{ key: p.key }}>
              <span className="fy-list__icon">
                <Icon of={BookOpen} size={20} />
              </span>
              <span className="fy-list__main">
                <span className="fy-list__title whitespace-normal">{p.title}</span>
              </span>
            </Link>
          </li>
        ))}
      </ul>
      {found.length === 0 && <p className="fy-secondary">No page holds every word you typed. Try fewer words, or ask the assistant.</p>}
    </div>
  );
}

// whatsNewKey is What's new's page: the release notes rather than a page of docs/user-guide/.
const whatsNewKey = "whats-new";

// HelpPage is one page of the guide at /help/$key, deep-linked to a heading by its anchor, with its sections in a
// contents column, and printable (FR-UX-32). /help/whats-new is the release notes.
export function HelpPage() {
  const { key } = useParams({ strict: false }) as { key?: string };
  const page =
    key === whatsNewKey
      ? { key, title: "What's new", draft: false, body: whatsNew || "No release has been published yet: each release's notes will be here, newest first." }
      : pageOf(key ?? "");
  useEffect(() => {
    const anchor = window.location.hash.slice(1);
    if (anchor) document.getElementById(anchor)?.scrollIntoView();
  }, [key]);
  if (!page) {
    return (
      <div className="fy-page fy-enter">
        <h1 className="fy-h1">No such page</h1>
        <p className="fy-secondary">
          The guide has no page named {key}. <Link to="/help">Open the guide's index</Link>.
        </p>
      </div>
    );
  }
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <Link to="/help" className="fy-small fy-row gap-1 print:hidden">
            <Icon of={ChevronLeft} />
            The guide
          </Link>
          <h1 className="fy-h1">{page.title}</h1>
          {page.draft && <p className="fy-small fy-secondary">Written while the pages it describes are built; reviewed when they are.</p>}
        </div>
        <Button variant="secondary" className="print:hidden" onClick={() => window.print()}>
          <Icon of={Printer} />
          Print
        </Button>
      </div>
      <div className="fy-help-layout">
        {headings(page.body).length > 1 && (
          <nav className="fy-help-layout__contents fy-small" aria-label="On this page">
            <ul className="fy-stack fy-stack--1">
              {headings(page.body).map((h) => (
                <li key={h.anchor}>
                  <a href={`#${h.anchor}`}>{h.text}</a>
                </li>
              ))}
            </ul>
          </nav>
        )}
        <article className="fy-help fy-stack fy-stack--3">
          <Markdown source={page.body} />
        </article>
      </div>
    </div>
  );
}
