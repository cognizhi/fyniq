import type { Meta, StoryObj } from "@storybook/react-vite";
import { Button } from "./button";
import { Dialog } from "./dialog";

// The dialog (MASTER.md §7): a sheet from the bottom in the compact class, centred from medium, its focus kept inside.
const meta = {
  title: "Shared/Dialog",
  component: Dialog,
  args: { open: true, onOpenChange: () => {}, title: "Void INV-0042?" },
} satisfies Meta<typeof Dialog>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Confirmation: Story = {
  args: {
    eyebrow: "Invoice",
    description: "Voiding posts a reversal dated today. The invoice keeps its number and shows as void.",
    children: <p>S$1,090.00 to Acme Analytics Pte Ltd, issued 14 Oct 2026.</p>,
    footer: (
      <>
        <Button variant="ghost">Keep it</Button>
        <Button variant="destructive">Void invoice</Button>
      </>
    ),
  },
};

export const WhileItRuns: Story = {
  args: { title: "Signing with your passkey", dismissible: false, description: "Follow your device's prompt to approve." },
};
