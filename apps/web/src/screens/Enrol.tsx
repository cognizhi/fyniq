import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, Circle, KeyRound, ShieldCheck } from "lucide-react";
import { api } from "../api/client";
import { buttonVariants } from "../components/ui/button";
import { helpText } from "../i18n";
import * as passkey from "../passkey";
import { Icon } from "../ui";
import { MyRecoveryCodes } from "./Recovery";
import { listPasskeys, Passkeys } from "./settings/Profile";

// Set up how you confirm it's you (SEC-01, SEC-35; docs/design/screens MfaEnrolment): a guided page of three steps that the
// Profile's cards also hold one by one, so a new person is walked through them in order: a passkey (or, where the browser
// cannot make one, the authenticator app the sign-in provider asks for), ten recovery codes kept safe, and a second
// authenticator, which is recommended. Each step says whether it is done, from what the API holds, and nothing here changes
// what a person may do: a second factor was asked for at sign-in already.

async function codeStatus() {
  const { data, error } = await api.GET("/v1/session/recovery-codes");
  if (error) throw error;
  return data;
}

function Step({ n, title, done, optional, children }: { n: number; title: string; done: boolean; optional?: boolean; children: React.ReactNode }) {
  return (
    <li className="fy-card" aria-labelledby={`enrol-${n}`}>
      <div className="fy-card__head">
        <h2 className="fy-h3 fy-row gap-2" id={`enrol-${n}`}>
          <Icon of={done ? CircleCheck : Circle} size={20} />
          Step {n}: {title}
          {optional && <span className="fy-small fy-secondary">Recommended</span>}
        </h2>
        <span className={done ? "fy-chip fy-chip--good" : "fy-chip fy-chip--neutral"}>{done ? "Done" : "To do"}</span>
      </div>
      <div className="fy-card__body fy-stack fy-stack--2">{children}</div>
    </li>
  );
}

export function Enrol() {
  const h = helpText("auth.enrol");
  const keys = useQuery({ queryKey: ["passkeys"], queryFn: listPasskeys });
  const codes = useQuery({ queryKey: ["recovery-codes"], queryFn: codeStatus });
  const live = (keys.data ?? []).filter((k) => !k.revoked_at).length;
  const unused = codes.data?.unused ?? 0;
  const canPasskey = passkey.supported();
  const firstDone = live >= 1;
  const codesDone = unused > 0;
  const secondDone = live >= 2;
  const complete = firstDone && codesDone;
  return (
    <div className="fy-stack fy-stack--4">
      <header className="fy-stack fy-stack--2">
        <h1 className="fy-h1 fy-row gap-2">
          <Icon of={ShieldCheck} size={24} />
          {h.label}
        </h1>
        <p className="fy-lead">{h.tooltip}</p>
      </header>
      <ol className="fy-stack fy-stack--3" aria-label="Steps">
        <Step n={1} title="Add a passkey" done={firstDone}>
          {!canPasskey && (
            <p className="fy-banner fy-banner--info" role="note">
              This browser cannot make passkeys. Use the authenticator app your sign-in provider asks for, and add a passkey later from a browser that can.
            </p>
          )}
          {canPasskey && <p className="fy-secondary">A passkey is your fingerprint, face or PIN on this device. It confirms it's you for approvals, payments and bank details with one touch.</p>}
          {canPasskey && <Passkeys />}
        </Step>
        <Step n={2} title="Keep your recovery codes" done={codesDone}>
          <p className="fy-secondary">Ten one-time codes, shown once. With an Owner's or Administrator's confirmation, one gives your account back if you lose every authenticator. Print them or keep them in a password manager.</p>
          <MyRecoveryCodes />
        </Step>
        <Step n={3} title="Add a second authenticator" done={secondDone} optional>
          <p className="fy-secondary">A second passkey on another device, or a passkey and the authenticator app, so losing one device does not lock you out.</p>
          {firstDone && !secondDone && canPasskey && <p className="fy-small fy-secondary">Add another passkey from step 1 on your other device.</p>}
        </Step>
      </ol>
      <section className="fy-card" aria-labelledby="enrol-done">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="enrol-done">
            {complete ? "You are set up" : "Not finished yet"}
          </h2>
        </div>
        <div className="fy-card__body fy-stack fy-stack--2">
          <p className="fy-secondary">
            {complete
              ? "Fyniq will ask you to confirm it's you, with your passkey, for approvals, releasing payments and changing bank details."
              : "Finish steps 1 and 2. You can come back to this page any time from Settings › Profile and security."}
          </p>
          <div className="fy-row gap-2">
            <Link className={buttonVariants({ variant: complete ? "primary" : "secondary", size: "sm" })} to="/">
              Go to Home
            </Link>
            <Link className={buttonVariants({ variant: "ghost", size: "sm" })} to="/settings/profile">
              <Icon of={KeyRound} />
              Profile and security
            </Link>
          </div>
        </div>
      </section>
    </div>
  );
}
