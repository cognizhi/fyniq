import register from "../../../docs/prd/reviews/review-status.json";

// The "Needs additional verification" caveat (NM-UX-06, FR-UX-41): while an area of rules awaits its second review, every page the review
// register lists for it says so at the top. The register is read at build time; sign-in and the other pages it never lists carry none.
export interface Area {
  id: string;
  status: string;
  short: string;
  message: string;
  screens: string[];
}
export interface Register {
  heading: string;
  help_key: string;
  never: string[];
  areas: Area[];
}

export const review: Register = register as Register;

export function reviewsFor(screen: string, r: Register = review): Area[] {
  return r.never.includes(screen) ? [] : r.areas.filter((a) => a.status === "pending" && a.screens.includes(screen));
}
