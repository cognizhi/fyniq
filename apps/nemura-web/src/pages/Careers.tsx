import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/schema";
import { helpText, t } from "../i18n";

type View = components["schemas"]["CareersView"];

// Careers is the public face of nm-hiring (NM-REC-02): a company's open roles at /careers/<address>, read by anyone with no sign-in. It shows the company's name and words and each published post's title,
// team, text and day, and no salary and no person. An address that is unknown or switched off answers alike.
export function Careers({ slug }: { slug: string }) {
  const [view, setView] = useState<View | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    void api.GET("/v1/careers/{slug}", { params: { path: { slug } } }).then(({ data }) => (data ? setView(data) : setFailed(true)), () => setFailed(true));
  }, [slug]);
  return (
    <main className="nm-shell__main" id="main">
      <h1 className="fy-h1" title={helpText("careers.page").tooltip}>{view?.found ? view.company : t("careers.title")}</h1>
      {!view && !failed && <p role="status" className="fy-secondary">{t("careers.loading")}</p>}
      {failed && <p role="alert" className="fy-secondary">{t("careers.notFound")}</p>}
      {view && !view.found && <p role="alert" className="fy-secondary">{t("careers.notFound")}</p>}
      {view?.found && (
        <>
          {view.intro && <p className="fy-secondary">{view.intro}</p>}
          {view.posts.length === 0 && <p className="fy-secondary">{t("careers.none")}</p>}
          <ul className="fy-stack fy-stack--3">
            {view.posts.map((p) => (
              <li key={p.id} className="fy-card fy-card--pad fy-stack fy-stack--2">
                <h2 className="fy-h2">{p.title}</h2>
                <span className="fy-secondary">{p.department} · {t("careers.published", { on: p.published_on })}</span>
                <p style={{ whiteSpace: "pre-wrap" }}>{p.body}</p>
              </li>
            ))}
          </ul>
        </>
      )}
    </main>
  );
}
