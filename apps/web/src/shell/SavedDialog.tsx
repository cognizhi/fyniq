import { useRouter, useRouterState } from "@tanstack/react-router";
import { Bookmark, Star, Trash2 } from "lucide-react";
import { useState } from "react";
import { Button } from "../components/ui/button";
import { Dialog } from "../components/ui/dialog";
import { helpText } from "../i18n";
import { hrefOf, useSaveActions, useSavedItems, type SavedItem } from "../saved";
import { Icon } from "../ui";

// The dialog of Favourites and views (see SavedMenu.tsx), loaded on demand.
export default function SavedDialogBody({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const h = helpText("shell.saved");
  const router = useRouter();
  const location = useRouterState({ select: (s) => s.location });
  const items = useSavedItems();
  const actions = useSaveActions();
  const [name, setName] = useState("");
  const [said, setSaid] = useState("");
  const list = items.data ?? [];
  const pinned = list.find((i) => i.kind === "favourite" && i.id === location.pathname);
  const query = location.searchStr ?? "";
  const go = (i: SavedItem) => {
    onOpenChange(false);
    router.history.push(hrefOf(i));
  };
  const toggle = async () => {
    const ok = pinned ? await actions.takeAway(pinned) : await actions.save({ kind: "favourite", title: document.title.replace(/ · Fyniq$/, "") || location.pathname, path: location.pathname });
    setSaid(ok ? (pinned ? "Taken out of your favourites." : "Added to your favourites.") : "That page could not be saved: it may not be part of your access.");
  };
  const saveView = async () => {
    const title = name.trim();
    if (!title) {
      setSaid("Give the view a name first.");
      return;
    }
    const ok = await actions.save({ kind: "view", title, path: location.pathname, query });
    if (ok) setName("");
    setSaid(ok ? `Saved the view "${title}".` : "That view could not be saved: it may not be part of your access, or you keep the most there can be.");
  };
  const group = (title: string, kind: SavedItem["kind"], empty: string) => {
    const mine = list.filter((i) => i.kind === kind);
    return (
      <section className="fy-stack fy-stack--2" aria-labelledby={`saved-${kind}`}>
        <h3 className="fy-eyebrow" id={`saved-${kind}`}>
          {title}
        </h3>
        {mine.length === 0 ? (
          <p className="fy-small fy-secondary">{empty}</p>
        ) : (
          <ul className="fy-list" aria-labelledby={`saved-${kind}`}>
            {mine.map((i) => (
              <li key={`${i.kind}:${i.id}`} className="fy-row">
                <button type="button" className="fy-list__item fy-grow text-start" onClick={() => go(i)}>
                  <span className="fy-list__icon">
                    <Icon of={kind === "favourite" ? Star : Bookmark} size={20} />
                  </span>
                  <span className="fy-list__main">
                    <span className="fy-list__title">{i.title}</span>
                    <span className="fy-list__meta">{hrefOf(i)}</span>
                  </span>
                </button>
                <Button variant="ghost" size="icon" aria-label={`Take ${i.title} away`} onClick={() => void actions.takeAway(i)}>
                  <Icon of={Trash2} size={20} />
                </Button>
              </li>
            ))}
          </ul>
        )}
      </section>
    );
  };
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title={h.label} description={h.tooltip}>
      <div className="fy-stack fy-stack--4">
        <div className="fy-row fy-row--wrap gap-2">
          <Button variant="secondary" size="sm" onClick={() => void toggle()} aria-pressed={!!pinned}>
            <Icon of={Star} />
            {pinned ? "Remove this page from favourites" : "Add this page to favourites"}
          </Button>
        </div>
        <form
          className="fy-row fy-row--wrap gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            void saveView();
          }}
        >
          <label className="fy-field fy-grow">
            <span className="fy-label">Save this page{query ? " with its filters" : ""} as a view</span>
            <input className="fy-input" value={name} onChange={(e) => setName(e.target.value)} maxLength={80} placeholder="A name, such as Overdue this quarter" />
          </label>
          <Button type="submit" variant="secondary" size="sm" className="self-end">
            Save the view
          </Button>
        </form>
        <p className="fy-small fy-secondary" role="status">
          {said}
        </p>
        {group("Favourites", "favourite", "Pin the pages you open most.")}
        {group("Views", "view", "Save a page with its filters, columns and sort to come back to it. A view's link is its address: whoever opens it sees only what their own roles let them see.")}
      </div>
    </Dialog>
  );
}
