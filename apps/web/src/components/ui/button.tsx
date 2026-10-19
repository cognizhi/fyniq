import { Button as ButtonPrimitive } from "@base-ui/react/button";
import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";
import { cn } from "../../lib/utils";

// Button (shadcn/ui on Base UI; MASTER.md §7): primary (one per view, brand teal), secondary
// (outline), ghost and destructive. Never AI violet. Targets are at least 24 × 24 px, and 44 × 44
// px on touch screens (WCAG 2.2 SC 2.5.8, FR-UX-37).
export const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-2 rounded-md border border-transparent font-medium whitespace-nowrap transition-colors duration-(--duration-fast) select-none disabled:pointer-events-none disabled:opacity-60 pointer-coarse:min-h-(--touch-target) pointer-coarse:min-w-(--touch-target) [&_svg]:pointer-events-none [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        primary: "bg-primary text-primary-foreground shadow-1 hover:bg-primary-hover",
        secondary: "border-border-strong bg-surface-1 text-foreground hover:bg-surface-2",
        ghost: "bg-transparent text-foreground hover:bg-surface-3",
        destructive: "border-destructive bg-transparent text-destructive hover:bg-status-critical-subtle",
      },
      size: {
        default: "h-9 px-3 text-base",
        sm: "h-8 px-2.5 text-sm",
        icon: "size-9",
      },
    },
    defaultVariants: { variant: "secondary", size: "default" },
  },
);

type Props = Omit<ComponentProps<typeof ButtonPrimitive>, "className"> & VariantProps<typeof buttonVariants> & { className?: string };

export function Button({ className, variant, size, ...props }: Props) {
  return <ButtonPrimitive data-slot="button" className={cn(buttonVariants({ variant, size }), className)} {...props} />;
}
