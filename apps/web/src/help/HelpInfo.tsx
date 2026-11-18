import { Link } from "@tanstack/react-router";
import { HelpPopover } from "../components/HelpPopover";
import { helpText } from "../i18n";
import { registry, type Mode } from "../registry";
import { guideLink } from "./links";

// HelpInfo is the ⓘ beside a label (FR-UX-30): its help key's title and plain words, an example, in Expert mode the
// accounting detail under a divider, and "Learn more" to the guide page the registry names for the key.
export function HelpInfo({ help, mode }: { help: string; mode: Mode }) {
  const h = helpText(help);
  const guide = registry.find((e) => e.help === help && e.guide)?.guide;
  const to = guide ? guideLink(guide) : undefined;
  return (
    <HelpPopover
      title={h.label}
      learnMore={
        to && (
          <Link className="fy-small" to="/help/$key" params={{ key: to.to.replace(/^\/help\//, "") }} hash={to.hash}>
            Learn more
          </Link>
        )
      }
    >
      <span className="fy-small text-foreground whitespace-normal">{h.detail ?? h.tooltip}</span>
      {h.example && <span className="fy-small fy-secondary whitespace-normal">For example: {h.example}</span>}
      {mode === "expert" && h.expert && <span className="fy-small fy-muted whitespace-normal border-t border-border pt-2">{h.expert}</span>}
    </HelpPopover>
  );
}
