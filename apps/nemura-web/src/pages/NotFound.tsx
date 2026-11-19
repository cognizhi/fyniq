import { t } from "../i18n";

export function NotFound({ home }: { home: string }) {
  return (
    <main className="nm-signin__main" id="main">
      <div className="nm-signin__card">
        <h1 className="fy-h1">{t("notfound.title")}</h1>
        <p className="fy-secondary">{t("notfound.fix")}</p>
        <a className="fy-btn fy-btn--primary" href={home}>{t("notfound.home")}</a>
      </div>
    </main>
  );
}
