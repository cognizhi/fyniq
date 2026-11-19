import { useState } from "react";
import { mondayOf, TimeGrid, today } from "./TimeGrid";

// MyTime is a person's own time in the personal workspace (FR-PRJ-05, FR-UX-39; docs/design/screens PersonalTime): their
// week, the timer, and what they sent for approval. Only their own time.
export function MyTime() {
  const [week, setWeek] = useState(mondayOf(today()));
  return (
    <div className="fy-page fy-enter">
      <div className="fy-pagehead">
        <div className="fy-pagehead__text">
          <h1 className="fy-h1">My time</h1>
          <p className="fy-secondary">Log your time by project and task, then send each week to the project's manager. Only you see and change your own time.</p>
        </div>
      </div>
      <TimeGrid week={week} onWeek={setWeek} />
    </div>
  );
}
