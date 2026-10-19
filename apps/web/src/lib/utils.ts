import { clsx, type ClassValue } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

// The app's text-* sizes are the --fs-* tokens (tokens.css): text-md is a size here, not a colour,
// so merging "text-md text-primary" keeps both.
const merge = extendTailwindMerge({ extend: { theme: { text: ["md"] } } });

// cn joins class names and lets a later Tailwind utility replace an earlier one (shadcn/ui).
export function cn(...inputs: ClassValue[]): string {
  return merge(clsx(inputs));
}
