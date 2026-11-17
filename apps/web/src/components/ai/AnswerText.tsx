import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import type { components } from "../../api/schema";

type Segment = components["schemas"]["AISegment"];

// AnswerText renders an answer's segments (AI-08, AI-69; p1-ai-assistant design D7): words in Fyniq's Markdown subset,
// each figure as Fyniq rendered it from its fact, and what could not be established. The subset is paragraphs, line
// breaks, "- " items, **bold**, *italic*, `code` and links to Fyniq's own pages; the API has already dropped images
// and raw HTML and written any other link out in full. Everything is rendered as text, never as HTML, so nothing the
// model writes can fetch, run or style anything.
export function AnswerText({ segments }: { segments: Segment[] }) {
  const paragraphs: ReactNode[][] = [[]];
  const add = (n: ReactNode) => paragraphs[paragraphs.length - 1]?.push(n);
  segments.forEach((s, i) => {
    if (s.fact) {
      add(
        <span key={`f${i}`} className="fy-fact">
          {s.figure}
        </span>,
      );
      return;
    }
    if (s.missing) {
      add(<span key={`m${i}`} className="fy-secondary">{`(I could not establish ${s.missing.trim().replace(/\.$/, "")})`}</span>);
      return;
    }
    (s.text ?? "").split(/\n{2,}/).forEach((part, p) => {
      if (p > 0) paragraphs.push([]);
      part.split("\n").forEach((line, l) => {
        if (l > 0) add(<br key={`b${i}.${p}.${l}`} />);
        const item = /^\s*[-*] /.exec(line);
        if (item) add(<span key={`d${i}.${p}.${l}`} aria-hidden="true">{"• "}</span>);
        inline(item ? line.slice(item[0].length) : line, `t${i}.${p}.${l}`).forEach(add);
      });
    });
  });
  return (
    <>
      {paragraphs
        .filter((p) => p.length > 0)
        .map((p, i) => (
          <p key={i} className="fy-ai-text">
            {p}
          </p>
        ))}
    </>
  );
}

const token = /\*\*([^*]+)\*\*|\*([^*]+)\*|_([^_]+)_|`([^`]+)`|\[([^\]]+)\]\((\/[^)\s]*)\)/g;

// inline renders one line's emphasis, code and links to Fyniq's own pages.
function inline(text: string, key: string): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  for (const m of text.matchAll(token)) {
    const at = m.index ?? 0;
    if (at > last) out.push(text.slice(last, at));
    const [, bold, em, em2, code, label, href] = m;
    const k = `${key}.${at}`;
    if (bold) out.push(<strong key={k}>{bold}</strong>);
    else if (em ?? em2) out.push(<em key={k}>{em ?? em2}</em>);
    else if (code) out.push(<code key={k} className="fy-mono">{code}</code>);
    else if (label && href && !href.startsWith("//")) out.push(<Link key={k} to={href}>{label}</Link>);
    else out.push(m[0]);
    last = at + m[0].length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}
