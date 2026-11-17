import type { Meta, StoryObj } from "@storybook/react-vite";
import { ReviewBanner } from "./ReviewBanner";

// The "Needs additional verification" caveat (FR-UX-41): on a screen the review register lists while its area awaits
// review, and nothing on one it does not list.
const meta = { title: "Shared/ReviewBanner", component: ReviewBanner } satisfies Meta<typeof ReviewBanner>;
export default meta;
type Story = StoryObj<typeof meta>;

export const AwaitingReview: Story = { args: { screen: "fixed-assets" } };

export const NotListed: Story = { args: { screen: "home" } };
