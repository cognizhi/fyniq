import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  CircleCheck,
  Fingerprint,
  Globe,
  KeyRound,
  LaptopMinimal,
  Link2,
  LogOut,
  MonitorCheck,
  Plus,
  ShieldCheck,
  Smartphone,
  Trash2,
  Unlink,
} from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, type Session, type SignInProvider } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { browserOf, clock, date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { setMode } from "../../mode";
import * as passkey from "../../passkey";
import type { Mode } from "../../registry";
import { linkURL, visit } from "../../route";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { MyBankAccount } from "./MyBankAccount";
import { SettingsPage } from "./SettingsPage";

type Passkey = NonNullable<Awaited<ReturnType<typeof listPasskeys>>>[number];
type Device = NonNullable<Awaited<ReturnType<typeof listDevices>>>[number];
type Identity = NonNullable<Awaited<ReturnType<typeof listIdentities>>>[number];

async function listPasskeys() {
  const { data, error } = await api.GET("/v1/session/passkeys", { params: { query: { page_size: 100 } } });
  if (error) throw error;
  return data.items ?? [];
}

async function listDevices() {
  const { data, error } = await api.GET("/v1/session/devices", { params: { query: { page_size: 100 } } });
  if (error) throw error;
  return data.items ?? [];
}

async function listIdentities() {
  const { data, error } = await api.GET("/v1/session/identities", { params: { query: { page_size: 100 } } });
  if (error) throw error;
  return data.items ?? [];
}

async function listProviders(): Promise<SignInProvider[]> {
  const { data, error } = await api.GET("/v1/sign-in-providers", { params: { query: { page_size: 100 } } });
  if (error) throw error;
  return data.items ?? [];
}

// What a refused link means, by its code (internal/signin), in plain words; the code is shown too (ENG-23).
export const linkErrors: Record<string, string> = {
  "AUTH-SIGNIN-01": "That sign-in provider is not available any more, so nothing was linked. Choose another below.",
  "AUTH-SIGNIN-02": "The link took longer than 10 minutes, or began in another browser, so nothing was linked. Select Link again.",
  "AUTH-SIGNIN-03": "The provider's answer did not pass Fyniq's checks, so nothing was linked. Select Link again.",
  "AUTH-SIGNIN-04": "The sign-in service did not answer, so nothing was linked. Try again in a moment.",
  "AUTH-SIGNIN-05": "The link did not start from Fyniq, so nothing was linked. Select Link on this page.",
  "AUTH-SIGNIN-06": "Linking needs you to have confirmed it's you in the last 5 minutes, so nothing was linked. Select Link again: Fyniq asks you to confirm first.",
  "AUTH-SIGNIN-07": "That account signs someone in to Fyniq already, so it was not linked. At the provider, sign in with another account, then select Link.",
  "AUTH-SIGNIN-08": "That provider accepts only accounts on some email domains, and this account's email is on none of them, so it was not linked. Link your work account.",
};

// ModeChoice is the experience mode, also in the header (FR-UX-23; experience/modes).
function ModeChoice({ mode }: { mode: Mode }) {
  const h = helpText("shell.mode");
  const choices: [Mode, string, string][] = [
    ["basic", "Basic", "The essentials, in plain words. Safety information always shows: what cannot post, what waits for approval, locked periods and deadlines."],
    ["expert", "Expert", "Everything: accounts, rule IDs, the organisation's settings and every detail."],
  ];
  return (
    <section className="fy-card" aria-labelledby="mode-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="mode-title">
          {h.label}
        </h2>
      </div>
      <fieldset className="fy-fieldset fy-card__body">
        <legend className="fy-sr">{h.label}</legend>
        <p className="fy-small fy-secondary">{h.tooltip}</p>
        {choices.map(([m, label, about]) => (
          <label key={m} className="fy-row fy-row--top gap-3">
            <input type="radio" name="mode" value={m} checked={mode === m} onChange={() => setMode(m)} className="mt-1" />
            <span className="fy-stack gap-0">
              <span className="fy-strong">{label}</span>
              <span className="fy-small fy-secondary">{about}</span>
            </span>
          </label>
        ))}
      </fieldset>
    </section>
  );
}

// SecondFactor says whether the session confirmed it's you recently, and lets the person do it now.
function SecondFactor({ session }: { session: Session }) {
  const confirm = useConfirm();
  const until = session.stepped_up_until;
  const by = session.step_up_method === "passkey" ? "with a passkey" : "when you signed in";
  return (
    <div className="fy-row fy-row--wrap gap-3 border-b border-border p-4">
      <span className="fy-list__icon fy-list__icon--brand">
        <Icon of={ShieldCheck} size={20} />
      </span>
      <span className="fy-stack min-w-0 flex-[1_1_220px] gap-0">
        <span className="fy-strong">{until ? `You confirmed it's you ${by}` : "Changes ask you to confirm it's you"}</span>
        <span className="fy-small fy-secondary">
          {until ? `It counts until ${clock(until)}; after that, a change or an approval asks again.` : "A change or an approval asks for a passkey, or for signing in again, first."}
        </span>
      </span>
      <Button size="sm" onClick={() => void confirm()}>
        <Icon of={Fingerprint} />
        Confirm it's you now
      </Button>
    </div>
  );
}

function AddPasskey({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const nameId = useId();
  const [name, setName] = useState(() => browserOf(navigator.userAgent));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [nameError, setNameError] = useState("");

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setProblem(null);
    const trimmed = name.trim();
    if (!trimmed || trimmed.length > 100) {
      setNameError("Give the passkey a name of 1 to 100 characters, such as the device it is on.");
      return;
    }
    setNameError("");
    setBusy(true);
    try {
      const started = await guarded(confirm, () => api.POST("/v1/session/passkeys/challenge"));
      if (!started.data) {
        setProblem(problemOf(started.error));
        return;
      }
      let credential: Record<string, unknown>;
      try {
        credential = await passkey.create(started.data.options);
      } catch (err) {
        setProblem(
          passkey.cancelled(err)
            ? { detail: "The passkey was cancelled, or took too long.", fix: "Select Add the passkey again when you are ready." }
            : { detail: err instanceof Error ? err.message : "Your browser refused to make a passkey.", fix: "Try again, or use another browser or device." },
        );
        return;
      }
      const added = await api.POST("/v1/session/passkeys", { body: { challenge_id: started.data.challenge_id, credential, name: trimmed } });
      if (!added.data) {
        setProblem(problemOf(added.error));
        return;
      }
      await queries.invalidateQueries({ queryKey: ["passkeys"] });
      onOpenChange(false);
    } finally {
      setBusy(false);
    }
  };

  const h = helpText("passkey.add");
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      dismissible={!busy}
      eyebrow="Profile and security"
      title={h.label}
      description="Your device makes the passkey and keeps its private key; Fyniq keeps only the public half. You confirm with your fingerprint, face or device PIN."
      icon={
        <span className="fy-list__icon fy-list__icon--brand">
          <Icon of={Fingerprint} size={20} />
        </span>
      }
    >
      <form className="fy-stack fy-stack--3" onSubmit={submit} noValidate>
        {problem && <Refusal problem={problem} title="The passkey was not added" />}
        {!passkey.supported() && <p className="fy-small fy-secondary">This browser cannot make passkeys. Use a current browser on a device with a fingerprint reader, face recognition or a PIN.</p>}
        <div className="fy-field">
          <label className="fy-label" htmlFor={nameId}>
            Name
          </label>
          <input
            className="fy-input"
            id={nameId}
            value={name}
            maxLength={100}
            onChange={(e) => setName(e.target.value)}
            aria-invalid={nameError ? true : undefined}
            aria-describedby={`${nameId}-help${nameError ? ` ${nameId}-error` : ""}`}
          />
          <span className="fy-help" id={`${nameId}-help`}>
            To know it by, such as "Work laptop".
          </span>
          {nameError && (
            <span className="fy-error" id={`${nameId}-error`}>
              {nameError}
            </span>
          )}
        </div>
        <div className="fy-row fy-row--wrap justify-end">
          <Button variant="ghost" type="button" onClick={() => onOpenChange(false)} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={busy || !passkey.supported()}>
            <Icon of={Fingerprint} />
            {busy ? "Waiting for your device…" : "Add the passkey"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function RevokePasskey({ target, onClose }: { target: Passkey | null; onClose: () => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("passkey.revoke");
  const revoke = async () => {
    if (!target) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => api.DELETE("/v1/session/passkeys/{passkey_id}", { params: { path: { passkey_id: target.id } } }));
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    await queries.invalidateQueries({ queryKey: ["passkeys"] });
    onClose();
  };
  return (
    <Dialog
      open={target !== null}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={target ? `Revoke ${target.name}?` : "Revoke the passkey?"}
      description="It stops working at once, here and for approvals it has not signed yet. This cannot be undone: add a new passkey instead."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={revoke} disabled={busy}>
            <Icon of={Trash2} />
            {h.label}
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The passkey was not revoked" />}
    </Dialog>
  );
}

function Passkeys() {
  const keys = useQuery({ queryKey: ["passkeys"], queryFn: listPasskeys });
  const [adding, setAdding] = useState(false);
  const [revoking, setRevoking] = useState<Passkey | null>(null);
  const add = helpText("passkey.add");
  const revoke = helpText("passkey.revoke");
  const live = (keys.data ?? []).filter((k) => !k.revoked_at);
  return (
    <div className="fy-stack fy-stack--3 p-4">
      <div className="fy-row fy-row--wrap">
        <h3 className="fy-h3 fy-grow" id="passkeys-title">
          Passkeys
        </h3>
        <Button size="sm" onClick={() => setAdding(true)} title={add.tooltip}>
          <Icon of={Plus} />
          {add.label}
        </Button>
      </div>
      {keys.isError && <Refusal problem={problemOf(keys.error)} title="Your passkeys could not be listed" />}
      {keys.isPending && <p className="fy-small fy-secondary">Loading your passkeys…</p>}
      {keys.data && live.length === 0 && (
        <p className="fy-small fy-secondary">No passkey yet. Add one so that confirming it's you, and signing approvals, takes a touch instead of signing in again.</p>
      )}
      {live.length > 0 && (
        <ul className="fy-list rounded-md border border-border" aria-labelledby="passkeys-title">
          {live.map((k) => (
            <li key={k.id} className="fy-list__item">
              <span className="fy-list__icon">
                <Icon of={/phone|iphone|android/i.test(k.name) ? Smartphone : KeyRound} size={20} />
              </span>
              <span className="fy-list__main">
                <span className="fy-list__title whitespace-normal">{k.name}</span>
                <span className="fy-list__meta whitespace-normal">
                  Added {date(k.created_at)}
                  {k.last_used_at ? ` · used ${dateTime(k.last_used_at)}` : " · not used yet"}
                  {k.backed_up ? " · synced to your other devices" : ""}
                </span>
              </span>
              <Button variant="ghost" size="sm" onClick={() => setRevoking(k)} aria-label={`${revoke.label} ${k.name}`} title={revoke.tooltip}>
                <Icon of={Trash2} />
                <span className="fy-from-medium">{revoke.label}</span>
              </Button>
            </li>
          ))}
        </ul>
      )}
      <AddPasskey open={adding} onOpenChange={setAdding} />
      <RevokePasskey target={revoking} onClose={() => setRevoking(null)} />
    </div>
  );
}

function Devices() {
  const queries = useQueryClient();
  const devices = useQuery({ queryKey: ["devices"], queryFn: listDevices });
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [status, setStatus] = useState("");
  const [askOthers, setAskOthers] = useState(false);
  const [busy, setBusy] = useState(false);
  const one = helpText("device.sign-out");
  const others = helpText("device.sign-out-others");
  const list = devices.data ?? [];
  const otherCount = list.filter((d) => !d.current).length;

  const signOut = async (d: Device) => {
    setProblem(null);
    setBusy(true);
    const out = await api.DELETE("/v1/session/devices/{device_id}", { params: { path: { device_id: d.id } } });
    setBusy(false);
    if (out.error) {
      setProblem(problemOf(out.error));
      return;
    }
    setStatus(`${browserOf(d.browser)} is signed out.`);
    await queries.invalidateQueries({ queryKey: ["devices"] });
  };

  const signOutOthers = async () => {
    setProblem(null);
    setBusy(true);
    const out = await api.POST("/v1/session/devices/sign-out-others");
    setBusy(false);
    setAskOthers(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    setStatus(out.data.ended === 1 ? "1 other session is signed out." : `${out.data.ended} other sessions are signed out.`);
    await queries.invalidateQueries({ queryKey: ["devices"] });
  };

  return (
    <div className="fy-stack fy-stack--3 border-t border-border p-4">
      <div className="fy-row fy-row--wrap">
        <h3 className="fy-h3 fy-grow" id="devices-title">
          Your devices
        </h3>
        {otherCount > 0 && (
          <Button variant="destructive" size="sm" onClick={() => setAskOthers(true)} title={others.tooltip}>
            <Icon of={LogOut} />
            {others.label}
          </Button>
        )}
      </div>
      <p className="fy-small fy-secondary" role="status">
        {status}
      </p>
      {problem && <Refusal problem={problem} title="The session was not signed out" />}
      {devices.isError && <Refusal problem={problemOf(devices.error)} title="Your devices could not be listed" />}
      {devices.isPending && <p className="fy-small fy-secondary">Loading your devices…</p>}
      {list.length > 0 && (
        <ul className="fy-list fy-stagger rounded-md border border-border" aria-labelledby="devices-title">
          {list.map((d) => {
            const name = browserOf(d.browser);
            return (
              <li key={d.id} className="fy-list__item">
                <span className={d.current ? "fy-list__icon fy-list__icon--brand" : "fy-list__icon"}>
                  <Icon of={/iOS|Android/.test(name) ? Smartphone : LaptopMinimal} size={20} />
                </span>
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal" title={d.browser}>
                    {name}
                  </span>
                  <span className="fy-list__meta whitespace-normal">
                    Signed in {dateTime(d.signed_in_at)} with {d.provider} · last used {dateTime(d.last_seen_at)}
                    {d.step_up_method === "passkey" ? " · confirmed with a passkey" : ""}
                  </span>
                </span>
                {d.current ? (
                  <span className="fy-chip fy-chip--info">
                    <Icon of={MonitorCheck} />
                    This device
                  </span>
                ) : (
                  <Button variant="ghost" size="sm" onClick={() => void signOut(d)} disabled={busy} aria-label={`${one.label} ${name}, signed in ${dateTime(d.signed_in_at)}`} title={one.tooltip}>
                    <Icon of={LogOut} />
                    {one.label}
                  </Button>
                )}
              </li>
            );
          })}
        </ul>
      )}
      <span className="fy-help">Each sign-in also sends you an email, so you know when a new device signs in.</span>
      <Dialog
        open={askOthers}
        onOpenChange={setAskOthers}
        dismissible={!busy}
        title={`${others.label}?`}
        description={`${otherCount === 1 ? "1 other session ends" : `${otherCount} other sessions end`} at their next request. This browser stays signed in.`}
        footer={
          <>
            <Button variant="ghost" onClick={() => setAskOthers(false)} disabled={busy}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={signOutOthers} disabled={busy}>
              <Icon of={LogOut} />
              {others.label}
            </Button>
          </>
        }
      />
    </div>
  );
}

// RemoveSignIn asks before removing one of the person's sign-ins (FR-ADM-08), after confirming it's them when needed.
function RemoveSignIn({ target, onClose, onRemoved }: { target: Identity | null; onClose: () => void; onRemoved: (name: string) => void }) {
  const queries = useQueryClient();
  const confirm = useConfirm();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const h = helpText("identity.unlink");
  const remove = async () => {
    if (!target) return;
    setBusy(true);
    setProblem(null);
    const out = await guarded(confirm, () => api.DELETE("/v1/session/identities/{identity_id}", { params: { path: { identity_id: target.id } } }));
    setBusy(false);
    if (!out.data) {
      setProblem(problemOf(out.error));
      return;
    }
    await queries.invalidateQueries({ queryKey: ["identities"] });
    await queries.invalidateQueries({ queryKey: ["devices"] });
    onRemoved(target.provider_name);
    onClose();
  };
  return (
    <Dialog
      open={target !== null}
      onOpenChange={(o) => !o && onClose()}
      dismissible={!busy}
      title={target ? `Remove your ${target.provider_name} sign-in?` : "Remove the sign-in?"}
      description="That account stops signing you in to Fyniq, and the devices signed in with it are signed out. You can link it again later."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="destructive" onClick={remove} disabled={busy}>
            <Icon of={Unlink} />
            {h.label}
          </Button>
        </>
      }
    >
      {problem && <Refusal problem={problem} title="The sign-in was not removed" />}
    </Dialog>
  );
}

// SignIns are the accounts at sign-in providers that sign the person in (FR-ADM-08, rev 0.196): linked by signing in with
// one from here, after confirming it's them; removed here, never the one in use nor the last. The browser comes back from
// a link with ?linked=<provider> or ?link_error=<code>.
function SignIns() {
  const identities = useQuery({ queryKey: ["identities"], queryFn: listIdentities });
  const providers = useQuery({ queryKey: ["sign-in-providers"], queryFn: listProviders });
  const confirm = useConfirm();
  const [removing, setRemoving] = useState<Identity | null>(null);
  const back = new URLSearchParams(window.location.search);
  const [linked, setLinked] = useState(back.get("linked"));
  const [status, setStatus] = useState("");
  const failed = back.get("link_error");
  const nameOf = (id: string) => providers.data?.find((p) => p.id === id)?.name ?? id;
  const link = helpText("identity.link");
  const unlink = helpText("identity.unlink");
  const list = identities.data ?? [];

  const linkWith = async (p: SignInProvider) => {
    setStatus("");
    setLinked(null);
    // The link needs a second factor in the last 5 minutes, and the person still has to sign in at the provider: ask
    // first unless at least 2 minutes of it are left.
    const { data: s } = await api.GET("/v1/session");
    const until = s?.stepped_up_until ? Date.parse(s.stepped_up_until) : 0;
    if (until - Date.now() < 2 * 60_000 && !(await confirm())) return;
    visit(linkURL(p.id));
  };

  return (
    <div className="fy-stack fy-stack--3 border-t border-border p-4">
      <h3 className="fy-h3" id="sign-ins-title">
        Linked sign-in accounts
      </h3>
      <p className="fy-small fy-secondary" role="status">
        {linked ? `Your ${nameOf(linked)} sign-in is linked: it signs you in from now on.` : status}
      </p>
      {failed && <Refusal problem={{ code: failed, detail: linkErrors[failed] ?? "Nothing was linked. Select Link again." }} title="The sign-in was not linked" />}
      {identities.isError && <Refusal problem={problemOf(identities.error)} title="Your sign-ins could not be listed" />}
      {identities.isPending && <p className="fy-small fy-secondary">Loading your sign-ins…</p>}
      {list.length > 0 && (
        <ul className="fy-list rounded-md border border-border" aria-labelledby="sign-ins-title">
          {list.map((i) => (
            <li key={i.id} className="fy-list__item">
              <span className={i.current ? "fy-list__icon fy-list__icon--brand" : "fy-list__icon"}>
                <Icon of={Globe} size={20} />
              </span>
              <span className="fy-list__main">
                <span className="fy-list__title whitespace-normal">{i.provider_name}</span>
                <span className="fy-list__meta whitespace-normal">
                  {i.linked_at ? `Linked ${date(i.linked_at)}` : `Since ${date(i.added_at)}`}
                  {i.last_used_at ? ` · used ${dateTime(i.last_used_at)}` : " · not used yet"}
                </span>
              </span>
              {i.current ? (
                <span className="fy-chip fy-chip--info">
                  <Icon of={MonitorCheck} />
                  Signed in with it
                </span>
              ) : (
                <Button variant="ghost" size="sm" onClick={() => setRemoving(i)} aria-label={`${unlink.label} your ${i.provider_name} sign-in`} title={unlink.tooltip}>
                  <Icon of={Unlink} />
                  <span className="fy-from-medium">{unlink.label}</span>
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      {(providers.data ?? []).length > 0 && (
        <div className="fy-row fy-row--wrap gap-2" role="group" aria-label="Link another sign-in">
          {(providers.data ?? []).map((p) => (
            <Button key={p.id} variant="secondary" size="sm" onClick={() => void linkWith(p)} title={link.tooltip}>
              <Icon of={Link2} />
              {`${link.label} ${p.name}`}
            </Button>
          ))}
        </div>
      )}
      <span className="fy-help">Whichever account you sign in with, Fyniq still asks for your second factor. An account links only when you sign in with it here, never because its email matches.</span>
      <RemoveSignIn target={removing} onClose={() => setRemoving(null)} onRemoved={(name) => {
          setLinked(null);
          setStatus(`Your ${name} sign-in is removed.`);
        }}
      />
    </div>
  );
}

// Profile is Settings › Profile and security (docs/design/screens ProfilePreferences, MfaEnrolment):
// the experience mode, your bank account for reimbursements, whether you confirmed it's you, your
// passkeys, your linked sign-in accounts and your devices (FR-ADM-02, FR-ADM-03, FR-ADM-08, FR-AP-22,
// SEC-01). Appearance stays in the header's menu; formats, keyboard and recovery codes join this page
// with their operations.
export function Profile() {
  const { session, mode } = useSignedIn();
  return (
    <SettingsPage title="Profile and security" screen="profile-preferences" lead="How Fyniq behaves for you, and how you confirm it's you.">
      <ModeChoice mode={mode} />
      <MyBankAccount />
      <section className="fy-card" aria-labelledby="security-title">
        <div className="fy-card__head">
          <h2 className="fy-h3" id="security-title">
            Sign-in and security
          </h2>
          <span className="fy-chip fy-chip--good">
            <Icon of={CircleCheck} />
            Second factor on
          </span>
        </div>
        <SecondFactor session={session} />
        <Passkeys />
        <SignIns />
        <Devices />
      </section>
    </SettingsPage>
  );
}
