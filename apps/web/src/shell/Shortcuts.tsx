import { Dialog } from "@base-ui/react/dialog";
import { CircleX, Keyboard, X } from "lucide-react";
import { useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from "react";
import type { Session } from "../api/client";
import { buttonVariants } from "../components/ui/button";
import { helpText } from "../i18n";
import { setMode, usePinnedBasic } from "../mode";
import type { Mode } from "../registry";
import { available, inboxKeys, keysOf, normalise, problems, setShortcutSettings, spell, useShortcutSettings, usual, type Shortcut } from "../shortcuts";
import { Icon } from "../ui";

type Person = Pick<Session, "roles" | "features"> & { user_id?: string; super_admin?: boolean };

function Keys({ keys }: { keys: string }) {
  const parts = spell(keys);
  return (
    <span className="fy-row gap-1">
      {parts.map((k, i) => (
        <span key={i} className="fy-row gap-1">
          {i > 0 && <span className="fy-small fy-secondary">then</span>}
          <kbd className="fy-kbd">{k}</kbd>
        </span>
      ))}
    </span>
  );
}

function Group({ title, rows }: { title: string; rows: readonly (readonly [ReactNode, string])[] }) {
  const id = useId();
  if (rows.length === 0) return null;
  return (
    <section className="fy-stack fy-stack--2" aria-labelledby={id}>
      <h3 className="fy-eyebrow" id={id}>
        {title}
      </h3>
      <dl className="fy-grid fy-grid--2 gap-x-4 gap-y-1">
        {rows.map(([keys, what]) => (
          <div key={what} className="fy-row fy-row--between gap-3">
            <dt className="fy-small">{what}</dt>
            <dd>{keys}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

// ShortcutSheet is every keyboard shortcut for the person, opened with ? or from the command palette (FR-UX-06;
// docs/design/screens Shortcuts): the ones that work everywhere, then in Expert mode the areas and create pages their
// access opens, the Inbox's keys and the entry forms'. Basic mode shows the ones it keeps and offers Expert. The
// person turns the single-character shortcuts off or changes their keys below (SC 2.1.4).
export function ShortcutSheet({ open, onOpenChange, session, mode }: { open: boolean; onOpenChange: (open: boolean) => void; session: Person; mode: Mode }) {
  const h = helpText("shell.shortcuts");
  const settings = useShortcutSettings(session.user_id ?? "");
  const pinned = usePinnedBasic();
  const list = available(session, mode, settings);
  const row = (s: Shortcut) => [<Keys keys={s.keys} />, s.group === "sheet" ? "Show these shortcuts" : s.label] as const;
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-(--z-modal) bg-scrim" />
        <Dialog.Popup
          data-slot="dialog-popup"
          className="fixed inset-x-4 top-[8vh] z-(--z-modal) mx-auto max-h-[84vh] max-w-2xl overflow-y-auto rounded-lg border border-border bg-popover p-5 text-popover-foreground shadow-3 outline-none"
        >
          <div className="fy-stack fy-stack--4">
            <div className="fy-row fy-row--between">
              <Dialog.Title className="fy-h2 fy-row gap-2">
                <Icon of={Keyboard} size={20} />
                {h.label}
              </Dialog.Title>
              <Dialog.Close className={buttonVariants({ variant: "ghost", size: "icon" })} aria-label="Close">
                <Icon of={X} size={20} />
              </Dialog.Close>
            </div>
            <Dialog.Description className="fy-secondary">
              Shortcuts never fire while you type in a field. {settings.on ? "" : "Your single-character shortcuts are off: Ctrl+K, Ctrl+J and Alt+Shift+M still work."}
            </Dialog.Description>
            <Group
              title="Everywhere"
              rows={[
                [<kbd className="fy-kbd">Ctrl K</kbd>, "Search and commands (⌘K on a Mac)"],
                [<kbd className="fy-kbd">Ctrl J</kbd>, "The assistant (⌘J on a Mac)"],
                ...(pinned ? [] : ([[<kbd className="fy-kbd">Alt Shift M</kbd>, `Switch to ${mode === "basic" ? "Expert" : "Basic"} mode (Option Shift M on a Mac)`]] as [ReactNode, string][])),
                ...list.filter((s) => s.group === "sheet").map(row),
              ]}
            />
            {mode === "basic" && !pinned ? (
              <div className="fy-row fy-row--between fy-row--wrap gap-2">
                <p className="fy-small fy-secondary">Expert mode adds keys to go to each area and to start each kind of record.</p>
                <button type="button" className="fy-btn fy-btn--secondary fy-btn--sm" onClick={() => setMode("expert")}>
                  Switch to Expert
                </button>
              </div>
            ) : (
              <>
                <Group title="Go to" rows={list.filter((s) => s.group === "go").map(row)} />
                <Group title="Create" rows={list.filter((s) => s.group === "create").map(row)} />
                <Group title="In the Inbox, while a request has the focus" rows={inboxKeys.map(([k, what]) => [<kbd className="fy-kbd">{k}</kbd>, what] as const)} />
                <Group
                  title="In an invoice, quote, bill, purchase order, journal or claim"
                  rows={[
                    [<kbd className="fy-kbd">Enter</kbd>, "The next field"],
                    [<kbd className="fy-kbd">Ctrl Enter</kbd>, "Save it (⌘Enter on a Mac)"],
                  ]}
                />
              </>
            )}
            <hr className="fy-divider" />
            <ShortcutSettings session={session} mode={mode} />
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

// ShortcutSettings turns the person's single-character shortcuts off or on, and changes their keys (WCAG 2.1 SC 2.1.4;
// FR-UX-06): in the shortcut sheet and under Settings › Profile and security. A change applies to this person only.
export function ShortcutSettings({ session, mode }: { session: Person; mode: Mode }) {
  const user = session.user_id ?? "";
  const s = useShortcutSettings(user);
  const id = useId();
  const h = helpText("shortcuts.settings");
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [wrong, setWrong] = useState<Record<string, string>>({});
  const [said, setSaid] = useState("");
  const summary = useRef<HTMLDivElement>(null);
  const shown = available(session, mode, { ...s, on: true });
  const labels = Object.fromEntries(shown.map((x) => [x.id, x.group === "sheet" ? "the shortcut sheet" : x.label]));
  useEffect(() => {
    if (Object.keys(wrong).length) summary.current?.focus();
  }, [wrong]);
  const edit = () => {
    setDraft(Object.fromEntries(shown.map((x) => [x.id, spell(x.keys).join(" ")])));
    setWrong({});
    setSaid("");
    setEditing(true);
  };
  const save = (e: FormEvent) => {
    e.preventDefault();
    const keys = { ...s.keys };
    for (const [k, typed] of Object.entries(draft)) {
      const v = normalise(typed);
      if (v === usual[k]) delete keys[k];
      else keys[k] = v;
    }
    const found = problems(keys, labels);
    setWrong(found);
    if (Object.keys(found).length) return;
    setShortcutSettings(user, { ...s, keys });
    setEditing(false);
    setSaid("Your keys are saved. They apply to you only.");
  };
  return (
    <section className="fy-stack fy-stack--3" aria-labelledby={`${id}-title`}>
      <div className="fy-row fy-row--between gap-3">
        <span className="fy-stack fy-stack--1">
          <span className="fy-label" id={`${id}-title`}>
            {h.label}
          </span>
          <span className="fy-help" id={`${id}-help`}>
            {h.tooltip}
          </span>
        </span>
        <button
          className="fy-switch"
          type="button"
          role="switch"
          aria-checked={s.on}
          aria-labelledby={`${id}-title`}
          aria-describedby={`${id}-help`}
          onClick={() => {
            setShortcutSettings(user, { ...s, on: !s.on });
            setSaid(s.on ? "Single-character shortcuts are off." : "Single-character shortcuts are on.");
          }}
        />
      </div>
      {!editing ? (
        <div className="fy-row fy-row--wrap gap-2">
          <button type="button" className="fy-btn fy-btn--secondary fy-btn--sm" onClick={edit} disabled={!s.on}>
            Change the keys
          </button>
          {Object.keys(s.keys).length > 0 && (
            <button
              type="button"
              className="fy-btn fy-btn--ghost fy-btn--sm"
              onClick={() => {
                setShortcutSettings(user, { ...s, keys: {} });
                setSaid("The usual keys are back.");
              }}
            >
              Use the usual keys
            </button>
          )}
        </div>
      ) : (
        <form className="fy-stack fy-stack--3" onSubmit={save} noValidate aria-label="Change the keys">
          {Object.keys(wrong).length > 0 && (
            <div className="fy-banner fy-banner--critical" role="alert" tabIndex={-1} ref={summary} aria-labelledby={`${id}-wrong`}>
              <Icon of={CircleX} size={20} />
              <div className="fy-banner__text">
                <span className="fy-strong" id={`${id}-wrong`}>
                  Some keys need changing
                </span>
                <ul className="fy-small">
                  {Object.entries(wrong).map(([k, why]) => (
                    <li key={k}>
                      <a href={`#${id}-${k}`}>{labels[k] ?? k}</a>: {why}
                    </li>
                  ))}
                </ul>
              </div>
            </div>
          )}
          <p className="fy-small fy-secondary">One key, or two typed one after the other, such as G B.</p>
          <div className="fy-form fy-form--2">
            {shown.map((x) => (
              <div className="fy-field" key={x.id}>
                <label className="fy-label" htmlFor={`${id}-${x.id}`}>
                  {x.group === "sheet" ? "Show the shortcuts" : x.label}
                </label>
                <input
                  className="fy-input fy-mono"
                  id={`${id}-${x.id}`}
                  value={draft[x.id] ?? ""}
                  maxLength={12}
                  autoComplete="off"
                  spellCheck={false}
                  aria-invalid={wrong[x.id] ? true : undefined}
                  aria-describedby={wrong[x.id] ? `${id}-${x.id}-error` : undefined}
                  onChange={(e) => setDraft({ ...draft, [x.id]: e.target.value })}
                />
                {wrong[x.id] && (
                  <span className="fy-error" id={`${id}-${x.id}-error`}>
                    {wrong[x.id]}
                  </span>
                )}
              </div>
            ))}
          </div>
          <div className="fy-row gap-2">
            <button type="submit" className="fy-btn fy-btn--primary fy-btn--sm">
              Save the keys
            </button>
            <button type="button" className="fy-btn fy-btn--ghost fy-btn--sm" onClick={() => setEditing(false)}>
              Cancel
            </button>
          </div>
        </form>
      )}
      <p className="fy-small" role="status">
        {said}
      </p>
      {keysOf(s, "sheet") !== usual.sheet && !editing && (
        <p className="fy-small fy-secondary">
          The sheet opens with <Keys keys={keysOf(s, "sheet")} />.
        </p>
      )}
    </section>
  );
}
