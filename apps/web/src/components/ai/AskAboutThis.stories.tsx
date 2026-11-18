import type { Meta, StoryObj } from "@storybook/react-vite";
import { AskAboutThis } from "./AskAboutThis";
import { AssistantContext } from "./context";

// Ask about this (FR-UX-17): a person's action that opens the assistant with the record as its context, so no violet.
const meta = { title: "AI/AskAboutThis", component: AskAboutThis } satisfies Meta<typeof AskAboutThis>;
export default meta;
type Story = StoryObj<typeof meta>;

export const OnARecord: Story = {
  args: { context: { kind: "invoice", id: "i-1", label: "INV-0136 · Kopi Labs" } },
  render: (args) => (
    <AssistantContext.Provider value={{ openWith: () => {}, enabled: true }}>
      <AskAboutThis {...args} />
    </AssistantContext.Provider>
  ),
};
