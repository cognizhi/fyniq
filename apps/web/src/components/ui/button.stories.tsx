import type { Meta, StoryObj } from "@storybook/react-vite";
import { Plus, Trash2 } from "lucide-react";
import { Icon } from "../../ui";
import { Button } from "./button";

// The button (MASTER.md §7): one primary action a screen, the others secondary or ghost, a destructive one outlined;
// an icon-only button has its name.
const meta = { title: "Shared/Button", component: Button, args: { children: "Save draft" } } satisfies Meta<typeof Button>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Primary: Story = { args: { variant: "primary", children: "Post journal" } };
export const Secondary: Story = { args: { variant: "secondary" } };
export const Ghost: Story = { args: { variant: "ghost", children: "Cancel" } };
export const Destructive: Story = { args: { variant: "destructive", children: "Void invoice" } };
export const Small: Story = { args: { size: "sm", children: "Add line" } };
export const Disabled: Story = { args: { variant: "primary", disabled: true, children: "Post journal" } };
export const WithIcon: Story = {
  args: {
    variant: "primary",
    children: (
      <>
        <Icon of={Plus} /> New invoice
      </>
    ),
  },
};
export const IconOnly: Story = { args: { size: "icon", "aria-label": "Remove the line", children: <Icon of={Trash2} size={20} /> } };
