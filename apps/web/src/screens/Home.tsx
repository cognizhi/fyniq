import { useSignedIn } from "../session";

// Home is the first signed-in page: who is signed in and where. Its tiles and Inbox arrive with the
// modules (docs/design/screens AppShell); the signed-in layout (session.tsx) gives it the frame.
export function Home() {
  const { session, organisation } = useSignedIn();
  return (
    <div className="fy-page fy-page--narrow fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">Fyniq</h1>
          <p className="fy-secondary">Intelligence behind every number.</p>
        </div>
      </div>
      <p>The pages of your workspace arrive here, one module at a time.</p>
      <div role="status" aria-live="polite" className="api">
        <p>
          {organisation
            ? `Signed in to ${organisation.name} (books in ${organisation.base_currency}).`
            : "Signed in. You are not a member of an organisation yet."}
          {session.super_admin && " You are a Super admin of this installation."}
        </p>
      </div>
    </div>
  );
}
