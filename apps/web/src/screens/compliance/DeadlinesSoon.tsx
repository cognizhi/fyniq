import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CalendarClock, ChevronRight } from "lucide-react";
import { date } from "../../format/date";
import { helpText } from "../../i18n";
import { Icon } from "../../ui";
import { today } from "../purchases/BillsBoard";
import { addDays, calendar, ItemChip } from "./StatutoryCalendar";

// DeadlinesSoon is the Inbox's Deadlines section (FR-TAX-07, design D10): the statutory calendar's items overdue or due in
// the next 30 days with nothing recorded, for those who record the calendar's filings. They are tasks in FR-TAX-07's
// sense: a view of the calendar, not stored.
export function DeadlinesSoon() {
  const c = useQuery({ queryKey: ["calendar"], queryFn: () => calendar() });
  const limit = addDays(today(), 30);
  const due = (c.data?.deadlines ?? []).filter((it) => it.status === "overdue" || ((it.status === "due_soon" || it.status === "open") && it.due_on && it.due_on <= limit));
  if (!c.data) return null;
  return (
    <section className="fy-card" aria-labelledby="deadlines-title">
      <div className="fy-card__head">
        <h2 className="fy-h3" id="deadlines-title" title={helpText("inbox.deadlines").tooltip}>
          Deadlines
        </h2>
      </div>
      {due.length === 0 ? (
        <p className="fy-card__body fy-small fy-muted">No statutory deadline is overdue or due in the next 30 days.</p>
      ) : (
        <ul className="fy-list" aria-labelledby="deadlines-title">
          {due.slice(0, 8).map((it) => (
            <li key={it.key}>
              <Link className="fy-list__item" to="/compliance/calendar" hash={it.key}>
                <span className="fy-list__icon">
                  <Icon of={CalendarClock} size={20} />
                </span>
                <span className="fy-list__main">
                  <span className="fy-list__title whitespace-normal">{it.title}</span>
                  <span className="fy-list__meta">Due {date(it.due_on)}</span>
                </span>
                <ItemChip it={it} />
                <Icon of={ChevronRight} />
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
