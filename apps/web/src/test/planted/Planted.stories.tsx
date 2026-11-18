import type { Meta, StoryObj } from "@storybook/react-vite";

// A planted accessibility failure (p1-launch-readiness task 4.1): an image with no text and a button with no name. The
// Storybook run must fail on it, which `make storybook` checks; it is never among the shared components' stories.
function Unnamed() {
  return (
    <div>
      <img src="data:image/gif;base64,R0lGODlhAQABAAAAACw=" />
      <button type="button" />
    </div>
  );
}

const meta = { title: "Planted/Unnamed", component: Unnamed } satisfies Meta<typeof Unnamed>;
export default meta;
export const Violations: StoryObj<typeof meta> = {};
