import type { Meta, StoryObj } from "@storybook/react-vite";
import { InApp } from "../../test/story";
import { AssistantPanel } from "./AssistantPanel";

// The assistant panel (FR-UX-17, AI-08): asked about a record, in Expert mode, and with the AI switched off.
const meta = { title: "AI/AssistantPanel", component: AssistantPanel } satisfies Meta<typeof AssistantPanel>;
export default meta;
type Story = StoryObj<typeof meta>;

const args = { mode: "expert" as const, onClearContext: () => {}, onClose: () => {} };

export const AskingAboutARecord: Story = {
  args: { ...args, enabled: true, context: { kind: "invoice", id: "i-1", label: "INV-0136 · Kopi Labs" } },
  render: (a) => (
    <InApp>
      <AssistantPanel {...a} />
    </InApp>
  ),
};

export const Off: Story = {
  args: { ...args, enabled: false },
  render: (a) => (
    <InApp>
      <AssistantPanel {...a} />
    </InApp>
  ),
};
