import type { Session } from "../api/client";

// The Finance | People switch (PRD-001 FR-PPL-02; PRD-002 NM-UX-03): a person with rights in both apps crosses between them from the header. It is a door and never a grant: Nemura's own sign-in and roles
// decide what is behind it, an employee who only claims has no switch (their Claims tile opens the personal workspace), and each app keeps its own last page (the other app restores it, `workspace.ts`).
export function WorkspaceSwitch({ session }: { session: Pick<Session, "workspace"> }) {
  const url = session.workspace?.people_url;
  if (!url) return null;
  return (
    <nav aria-label="Workspace" className="fy-from-medium flex items-center gap-0.5 rounded-md border border-border bg-surface-1 p-0.5 text-sm" data-tour="workspace-switch">
      <span aria-current="page" className="rounded px-2 py-1 font-semibold">
        Finance
      </span>
      <a href={url} className="rounded px-2 py-1" title="Open the people workspace: Nemura signs you in with the same account and keeps its own last page">
        People
      </a>
    </nav>
  );
}
