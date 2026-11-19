import { LetterList } from "../components/LetterList";
import { ReviewBanner } from "../components/ReviewBanner";
import { helpText, t } from "../i18n";
import { Shell, admin, team } from "./Shell";

// Letters is nm-letters: every letter request for the people team and the signatory, the oldest due first, with the check, the reading, the passkey signature and the refusal.
export function Letters({ roles }: { roles: readonly string[] }) {
  return (
    <Shell roles={roles} directory={team(roles)} features={admin(roles)}>
      <main className="nm-shell__main" id="main">
        <ReviewBanner screen="nm-letters" />
        <h1 className="fy-h1" title={helpText("letters.page").tooltip}>{t("letters.title")}</h1>
        <p className="fy-secondary">{t("letters.lead")}</p>
        <LetterList mine={false} roles={roles} />
      </main>
    </Shell>
  );
}
