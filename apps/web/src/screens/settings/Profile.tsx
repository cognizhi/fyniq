import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleCheck, Fingerprint, KeyRound, LaptopMinimal, LogOut, MonitorCheck, Plus, ShieldCheck, Smartphone, Trash2 } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { api, type Session } from "../../api/client";
import { Refusal } from "../../components/Refusal";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { browserOf, clock, date, dateTime } from "../../format/date";
import { helpText } from "../../i18n";
import { setMode } from "../../mode";
import * as passkey from "../../passkey";
import type { Mode } from "../../registry";
import { useSignedIn } from "../../session";
import { guarded, useConfirm } from "../../stepup";
import { Icon, problemOf, type ApiProblem } from "../../ui";
import { SettingsPage } from "./SettingsPage";

type Passkey = NonNullable<Awaited<ReturnType<typeof listPasskeys>>>[number];
type Device = NonNullable<Awaited<ReturnType<typeof listDevices>>>[number];

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

// Profile is Settings › Profile and security (docs/design/screens ProfilePreferences, MfaEnrolment):
// the experience mode, whether you confirmed it's you, your passkeys and your devices (FR-ADM-02,
// FR-ADM-03, SEC-01). Appearance stays in the header's menu; formats, keyboard, linked sign-in
// accounts and recovery codes join this page with their operations.
export function Profile() {
  const { session, mode } = useSignedIn();
  return (
    <SettingsPage title="Profile and security" screen="profile-preferences" lead="How Fyniq behaves for you, and how you confirm it's you.">
      <ModeChoice mode={mode} />
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
        <Devices />
      </section>
    </SettingsPage>
  );
}
