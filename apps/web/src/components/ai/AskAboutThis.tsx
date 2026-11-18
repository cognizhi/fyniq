import { Sparkles } from "lucide-react";
import { helpText } from "../../i18n";
import { Icon } from "../../ui";
import { Button } from "../ui/button";
import type { TurnContext } from "./assistant";
import { useAssistant } from "./context";

// AskAboutThis opens the assistant panel with what the page shows as its context chip (FR-UX-17): a record, a report or a
// figure. It is a person's action, so it carries no violet; it is not shown while the AI is switched off, nor outside
// the signed-in frame.
export function AskAboutThis({ context, onAsk }: { context: TurnContext; onAsk?: () => void }) {
  const assistant = useAssistant();
  const h = helpText("assistant.ask-about-this");
  if (!assistant?.enabled) return null;
  return (
    <Button variant="secondary" title={h.tooltip} onClick={() => {
      onAsk?.();
      assistant.openWith(context);
    }}>
      <Icon of={Sparkles} />
      {h.label}
    </Button>
  );
}
