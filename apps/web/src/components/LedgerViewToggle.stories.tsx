import type { Meta, StoryObj } from "@storybook/react-vite";
import { LedgerViewToggle } from "./LedgerViewToggle";

// Plain view or Accountant view (FR-UX-08): offered in Expert mode only.
const meta = { title: "Shared/LedgerViewToggle", component: LedgerViewToggle } satisfies Meta<typeof LedgerViewToggle>;
export default meta;
type Story = StoryObj<typeof meta>;

export const InExpert: Story = { args: { mode: "expert" } };

export const InBasic: Story = { args: { mode: "basic" } };
