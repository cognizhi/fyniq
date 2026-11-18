import type { Meta, StoryObj } from "@storybook/react-vite";
import { AdvancedDetails } from "./AdvancedDetails";

// "Has advanced details (N)" (FR-UX-25): in Basic mode, a record's values in Expert's fields, read-only.
const meta = { title: "Shared/AdvancedDetails", component: AdvancedDetails } satisfies Meta<typeof AdvancedDetails>;
export default meta;
type Story = StoryObj<typeof meta>;

export const InBasic: Story = {
  args: {
    mode: "basic",
    details: [
      { label: "Line 1 · Department", value: "DEL Delivery" },
      { label: "Line 1 · Spread", value: "12 months from Oct 2026" },
    ],
  },
};

export const InExpert: Story = { args: { mode: "expert", details: [{ label: "Line 1 · Department", value: "DEL Delivery" }] } };
