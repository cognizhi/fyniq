import { SettingsList } from "./SettingsPage";

// Settings (docs/design/screens OrganisationSettings): the list of Settings pages, your own first,
// then the organisation's that your roles let you see. On a phone it is where Settings opens.
export function Settings() {
  return (
    <div className="fy-page fy-page--narrow fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">Settings</h1>
          <p className="fy-secondary">Your own settings, and the organisation's that your roles let you see.</p>
        </div>
      </div>
      <nav aria-label="Settings" className="fy-card fy-card--pad">
        <SettingsList compact />
      </nav>
    </div>
  );
}
