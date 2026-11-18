import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { HelpPopover } from "../components/HelpPopover";
import { entryOf } from "./glossary";

// Term is a glossary term in a page's words (FR-UX-11): underlined with dots, it shows its definition from the glossary on
// hover, focus or a tap, with "Learn more" to the glossary at its heading. A name the glossary lacks fails its test.
export function Term({ name, children }: { name: string; children?: ReactNode }) {
  const e = entryOf(name);
  if (!e) return <>{children ?? name}</>;
  return (
    <HelpPopover
      title={e.term}
      term={children ?? name}
      learnMore={
        <Link className="fy-small" to="/help/$key" params={{ key: "reference.glossary" }} hash={e.anchor}>
          Learn more in the glossary
        </Link>
      }
    >
      <span className="fy-small text-foreground whitespace-normal">{e.definition}</span>
    </HelpPopover>
  );
}
