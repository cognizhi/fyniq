import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { CircleCheck, Hourglass, TriangleAlert, UserX } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, newKey } from "../../api/client";
import type { components } from "../../api/schema";
import { Refusal } from "../../components/Refusal";
import { ReviewBanner } from "../../components/ReviewBanner";
import { Button } from "../../components/ui/button";
import { helpText } from "../../i18n";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";

type Person = components["schemas"]["PeopleProjectionPerson"];

// find reads the projection a page at a time until it meets the person: the list is the API's only read of it (FR-PPL-06), and a few hundred people are a few pages.
async function find(personId: string): Promise<Person | null> {
  let cursor: string | undefined;
  for (let page = 0; page < 40; page++) {
    const r = await api.GET("/v1/people-link/people", { params: { query: { cursor, page_size: 200 } } });
    if (r.error) throw r.error;
    const hit = (r.data.items ?? []).find((p) => p.person_id === personId);
    if (hit) return hit;
    cursor = r.data.next_cursor ?? undefined;
    if (!cursor) return null;
  }
  return null;
}

// MapUser says which Fyniq user this person is (FR-PPL-06): explicit, never by email, and only an Owner, an Administrator or the People and HR right may.
function MapUser({ person, onDone }: { person: Person; onDone: (what: string) => void }) {
  const id = useId();
  const h = helpText("people.map");
  const confirm = useConfirm();
  const [user, setUser] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const members = useQuery({
    queryKey: ["members"],
    queryFn: async () => {
      const r = await api.GET("/v1/members", {});
      if (r.error) throw r.error;
      return (r.data.items ?? []).filter((m) => !m.removed);
    },
  });
  const run = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () =>
      api.POST("/v1/people-link/people/{person_id}/user", { params: { path: { person_id: person.person_id }, header: { "Idempotency-Key": newKey() } }, body: { user_id: user } }),
    );
    setBusy(false);
    if (!out.data) return setProblem(problemOf(out.error));
    onDone(`${person.display_name ?? "The person"} is now this Fyniq user.`);
  };
  return (
    <form className="fy-stack fy-stack--2" onSubmit={(e) => void run(e)} noValidate aria-label="Map to a Fyniq user">
      {problem && <Refusal problem={problem} title="The person was not mapped" />}
      <div className="fy-field">
        <label className="fy-label" htmlFor={`${id}-user`}>
          Fyniq user
        </label>
        <select className="fy-select" id={`${id}-user`} value={user} onChange={(e) => setUser(e.target.value)}>
          <option value="">Choose the person's sign-in</option>
          {(members.data ?? []).map((m) => (
            <option key={m.user_id} value={m.user_id}>
              {m.display_name}
              {m.email ? `, ${m.email}` : ""}
            </option>
          ))}
        </select>
        <span className="fy-hint">Chosen by a person, never matched by email: it decides whose approvals and sign-ins count for this person.</span>
      </div>
      <div className="fy-row justify-end gap-2">
        <Button variant="secondary" type="submit" disabled={busy || !user} title={h.tooltip}>
          {h.label}
        </Button>
      </div>
    </form>
  );
}

// EmployeeRecord is one person as Fyniq holds them (docs/design/screens EmployeeRecord; FR-PPL-06, FR-PPL-13, FR-PPL-14): what Nemura told Fyniq, never pay, a bank number or an NRIC, and which Fyniq user they are.
export function EmployeeRecord() {
  useSignedIn();
  const queries = useQueryClient();
  const { personId } = useParams({ strict: false }) as { personId: string };
  const [status, setStatus] = useState("");
  const q = useQuery({ queryKey: ["people-projection", personId], queryFn: () => find(personId) });
  const p = q.data ?? undefined;
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <nav className="fy-breadcrumb" aria-label="Breadcrumb">
            <Link to="/payroll">Payroll</Link>
          </nav>
          <h1 className="fy-h1">{p?.display_name ?? "Person"}</h1>
          <p className="fy-secondary">What Nemura told Fyniq about this person, and which Fyniq user they are. Pay, bank numbers and NRIC stay in Nemura.</p>
        </div>
      </div>
      <ReviewBanner screen="employee-record" />
      {q.isError && <Refusal problem={problemOf(q.error)} title="The person could not be shown" />}
      {q.isSuccess && !p && <p className="fy-secondary">Nemura has not told Fyniq about this person.</p>}
      {status && (
        <div className="fy-banner fy-banner--good" role="status">
          <Icon of={CircleCheck} size={20} />
          <div className="fy-banner__text">
            <span className="fy-strong">{status}</span>
          </div>
        </div>
      )}
      {p && (
        <>
          <section aria-label="What the books use" className="fy-stack fy-stack--2">
            <h2 className="fy-h2">What the books use</h2>
            <dl className="fy-dl">
              <div>
                <dt>Status</dt>
                <dd>{p.removed ? "Removed in Nemura" : (p.status ?? "Active")}</dd>
              </div>
              <div>
                <dt>Department</dt>
                <dd>{p.department_id ? p.department_id : "None recorded"}</dd>
              </div>
              <div>
                <dt>Line manager</dt>
                <dd>{p.line_manager_person_id ? <Link to="/payroll/employees/$personId" params={{ personId: p.line_manager_person_id }}>Open their record</Link> : "None recorded"}</dd>
              </div>
            </dl>
            {p.held ? (
              <div className="fy-banner fy-banner--warning" role="note">
                <Icon of={TriangleAlert} size={20} />
                <div className="fy-banner__text">
                  <span className="fy-strong">Held for tax clearance.</span>
                  <span className="fy-small">Nothing is paid to this person until Nemura lifts the hold, after IRAS's notice or directive. Their salary lines wait.</span>
                </div>
              </div>
            ) : (
              <p>
                <Icon of={CircleCheck} /> No tax-clearance hold.
              </p>
            )}
            {p.removed && (
              <p className="fy-secondary">
                <Icon of={UserX} /> Removed in Nemura: no new line is accepted for them.
              </p>
            )}
          </section>
          <section aria-label="Fyniq access" className="fy-stack fy-stack--2">
            <h2 className="fy-h2">Fyniq access</h2>
            {p.user_id ? (
              <p>
                <Icon of={CircleCheck} /> Mapped to a Fyniq user. Their sign-in in Nemura is what lets them in here, with the roles an Owner gave them.
              </p>
            ) : (
              <p className="fy-secondary">
                <Icon of={Hourglass} /> Not mapped to a Fyniq user yet.
              </p>
            )}
            <MapUser
              person={p}
              onDone={(w) => {
                setStatus(w);
                void queries.invalidateQueries({ queryKey: ["people-projection", personId] });
              }}
            />
          </section>
        </>
      )}
    </div>
  );
}
