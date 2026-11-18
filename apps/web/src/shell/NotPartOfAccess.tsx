import { Lock } from "lucide-react";
import { Icon } from "../ui";

// NotPartOfAccess is what a link outside the person's access shows (docs/design/screens NotPartOfAccess; FR-UX-39,
// FR-UX-40, SEC-34): a plain page with a way back, never a partial page, names or figures. Presentation only: the API
// refuses the page's reads whatever is shown.
export function NotPartOfAccess({ personal }: { personal: boolean }) {
  return (
    <div className="fy-page fy-enter">
      <section className="fy-empty" aria-labelledby="not-part-title">
        <span className="fy-empty__art" aria-hidden="true">
          <Icon of={Lock} />
        </span>
        <h1 className="fy-h2" id="not-part-title">
          This page isn't part of your access
        </h1>
        <p className="fy-secondary">
          {personal
            ? "The link opens a page in Fyniq's finance workspace. Your access covers your own claims, time and shares."
            : "The link opens a part of Fyniq that your roles do not cover."}
        </p>
        <a className="fy-btn fy-btn--primary" href={personal ? "/me/claims" : "/"}>
          {personal ? "Go to My claims" : "Back to Home"}
        </a>
        <p className="fy-small fy-muted">If you need this page for your work, ask an Owner or an Administrator, who looks after access.</p>
      </section>
    </div>
  );
}
