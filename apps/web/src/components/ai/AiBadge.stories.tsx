import type { Meta, StoryObj } from "@storybook/react-vite";
import { AiBadge } from "./AiBadge";

// The AI badge (experience/visual-design AI accent): on everything the AI made, beside the text it marks.
const meta = { title: "Shared/AiBadge", component: AiBadge } satisfies Meta<typeof AiBadge>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Alone: Story = {};

export const BesideASuggestion: Story = {
  render: () => (
    <p className="fy-stack fy-stack--1">
      <AiBadge /> Categorise as 6110 Advertising, as the last three payments to this supplier were.
    </p>
  ),
};
