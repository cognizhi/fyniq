import { Sparkles } from "lucide-react";
import { Icon } from "../../ui";

// The AI badge (MASTER.md §7; experience/visual-design AI accent): Lucide sparkles and the word
// "AI" on the violet subtle background, on every AI message, proposal, suggestion and AI-drafted
// text until a person edits and saves it. Violet belongs to src/components/ai/ only: the token test
// fails when an AI token is used anywhere else, so nothing a person made looks like AI and AI
// content never looks like posted data.
export function AiBadge() {
  return (
    <span className="inline-flex items-center gap-1 rounded-sm bg-ai-subtle px-1.5 text-xs font-medium text-ai-subtle-foreground">
      <Icon of={Sparkles} />
      AI
    </span>
  );
}
