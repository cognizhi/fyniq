import { useQuery } from "@tanstack/react-query";
import { api } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { problemOf } from "../../ui";
import { count, vestingText, type OptionGrant } from "./optionWords";

async function myGrants(): Promise<{ linked: boolean; grants: OptionGrant[] }> {
  const { data, error } = await api.GET("/v1/me/option-grants", {});
  if (error) throw error;
  return { linked: data.linked, grants: data.grants ?? [] };
}

// MyShares is My shares and options (docs/design/screens MyShares; FR-EQ-19, FR-UX-39, Basic): the signed-in person's
// own grants, their vesting and what comes next, and nobody else's. Fyniq shows no cash value for options.
export function MyShares() {
  const q = useQuery({ queryKey: ["my-option-grants"], queryFn: myGrants });
  const helpH = helpText("equity.my-shares");
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <span className="fy-eyebrow">Personal</span>
          <h1 className="fy-h1" title={helpH.tooltip}>
            My shares and options
          </h1>
          <p className="fy-secondary">Your own option grants. You see only yours.</p>
        </div>
      </div>
      {q.isError && <Refusal problem={problemOf(q.error)} title="Your grants could not be shown" />}
      {q.data && q.data.grants.length === 0 && (
        <p className="fy-small fy-muted">
          {q.data.linked ? "You hold no recorded grant of options." : "No grant of options is linked to you. If you were granted options, ask the company to link your sign-in to you."}
        </p>
      )}
      {(q.data?.grants ?? []).map((g) => (
        <section key={g.id} className="fy-card" aria-labelledby={`grant-${g.id}`}>
          <div className="fy-card__head">
            <h2 className="fy-h3" id={`grant-${g.id}`}>
              Granted {date(g.granted_on)}
            </h2>
          </div>
          <div className="fy-card__body fy-stack fy-stack--3">
            <p>
              <span className="fy-tile__value fy-num">{count(g.vested)}</span> of <span className="fy-num">{count(g.number)}</span> options vested
            </p>
            <dl className="fy-dl">
              <div>
                <dt>Next vesting</dt>
                <dd>{g.next_vests_on ? `${count(g.next_vests)} on ${date(g.next_vests_on)}` : "Nothing more vests"}</dd>
              </div>
              <div>
                <dt>Exercise price</dt>
                <dd>
                  {g.currency} {g.exercise_price} a share
                </dd>
              </div>
              <div>
                <dt>Vesting</dt>
                <dd>{vestingText(g)}</dd>
              </div>
              <div>
                <dt>Exercised</dt>
                <dd className="fy-num">{count(g.exercised)}</dd>
              </div>
              <div>
                <dt>Expires on</dt>
                <dd>{date(g.expires_on)}</dd>
              </div>
            </dl>
            <p className="fy-small fy-secondary">
              To exercise vested options, ask the company's finance team. Fyniq shows no cash value for options: their worth depends on a future sale or
              funding round.
            </p>
          </div>
        </section>
      ))}
      <section className="fy-card fy-card--pad fy-stack fy-stack--2" aria-labelledby="only-yours">
        <h2 className="fy-h3" id="only-yours">
          Only your grant
        </h2>
        <p className="fy-small fy-secondary">The company's cap table and other people's grants are not part of your access.</p>
      </section>
    </div>
  );
}
