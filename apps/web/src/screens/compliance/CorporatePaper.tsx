// A corporate document's text, as its PDF lays it out (FR-CS-05; p1-company-secretary design D6): shown where a person
// reads a draft before having it signed, and where they review the AI's sec.draft_document proposal. Text only: nothing
// here works anything out.

export type PaperShape = {
  title: string;
  company?: string; // the API's name for the company at its head
  author?: string; // a dry run's, from the render layout
  lines?: string[] | null;
  banner?: string;
  sections?: { heading?: string; paragraphs?: string[] | null; items?: string[] | null }[] | null;
  signatures?: { name: string; role: string }[] | null;
  footer?: string;
};

// PaperView is a paper's text: the banner saying it is Fyniq's draft, the company at its head with its UEN and office,
// the title, each section's paragraphs and numbered items, and who signs it.
export function PaperView({ paper }: { paper: PaperShape }) {
  const signatures = paper.signatures ?? [];
  return (
    <article className="fy-stack fy-stack--2" aria-label={paper.title}>
      {paper.banner && <p className="fy-small fy-secondary">{paper.banner}</p>}
      <div>
        <p className="fy-strong">{paper.company ?? paper.author}</p>
        {(paper.lines ?? []).map((l) => (
          <p key={l} className="fy-small fy-muted">
            {l}
          </p>
        ))}
      </div>
      <h3 className="fy-h3">{paper.title}</h3>
      {(paper.sections ?? []).map((s, i) => (
        <div key={i} className="fy-stack fy-stack--1">
          {s.heading && <h4 className="fy-strong">{s.heading}</h4>}
          {(s.paragraphs ?? []).map((p, j) => (
            <p key={j}>{p}</p>
          ))}
          {(s.items ?? []).length > 0 && (
            <ol className="list-decimal ps-5 fy-stack fy-stack--1">
              {(s.items ?? []).map((x, j) => (
                <li key={j}>{x}</li>
              ))}
            </ol>
          )}
        </div>
      ))}
      {signatures.length > 0 && (
        <ul className="fy-stack fy-stack--1" aria-label="Signed by">
          {signatures.map((s, i) => (
            <li key={i} className="fy-small">
              {s.name}, {s.role}
            </li>
          ))}
        </ul>
      )}
      {paper.footer && <p className="fy-small fy-muted fy-mono">{paper.footer}</p>}
    </article>
  );
}
