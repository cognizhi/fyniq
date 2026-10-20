import { Dialog as Primitive } from "@base-ui/react/dialog";
import { X } from "lucide-react";
import { createContext, useContext, type ReactNode, type RefObject } from "react";
import { cn } from "../../lib/utils";
import { Icon } from "../../ui";
import { buttonVariants } from "./button";

// The element dialogs render into: the app's .fy-device wrapper, so the kit's container queries
// size them by device class (MASTER.md §5.1). Outside the app (component tests) it is the body.
export const DeviceRoot = createContext<RefObject<HTMLElement | null> | null>(null);

// Dialog (shadcn/ui on Base UI; MASTER.md §7, the kit's .fy-dialog): a sheet from the bottom in the
// compact class, centred from medium. Focus moves into it, stays there until it closes and returns
// to what opened it; Esc closes it unless `dismissible` is false (a ceremony in progress).
export function Dialog({
  open,
  onOpenChange,
  title,
  eyebrow,
  description,
  icon,
  children,
  footer,
  wide,
  dismissible = true,
  closeLabel = "Close",
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  eyebrow?: ReactNode;
  description?: ReactNode;
  icon?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
  wide?: boolean;
  dismissible?: boolean;
  closeLabel?: string;
}) {
  const device = useContext(DeviceRoot);
  return (
    <Primitive.Root open={open} onOpenChange={(o) => (o || dismissible ? onOpenChange(o) : undefined)} disablePointerDismissal={!dismissible}>
      <Primitive.Portal container={device ?? undefined}>
        <Primitive.Backdrop className="fixed inset-0 z-(--z-modal) bg-scrim" />
        <Primitive.Viewport className="fixed inset-0 z-(--z-modal) flex items-end justify-center medium:items-center medium:p-6">
          <Primitive.Popup data-slot="dialog-popup" className={cn("fy-dialog max-h-[92vh]", wide && "fy-dialog--wide")}>
            <div className="fy-grabber" aria-hidden="true" />
            <div className="fy-dialog__head">
              {icon}
              <div className="fy-stack fy-stack--2 fy-grow">
                {eyebrow && <span className="fy-eyebrow">{eyebrow}</span>}
                <Primitive.Title className="fy-h2">{title}</Primitive.Title>
                {description && <Primitive.Description className="fy-small fy-secondary">{description}</Primitive.Description>}
              </div>
              {dismissible && (
                <Primitive.Close className={buttonVariants({ variant: "ghost", size: "icon" })} aria-label={closeLabel}>
                  <Icon of={X} size={20} />
                </Primitive.Close>
              )}
            </div>
            {children && <div className="fy-dialog__body">{children}</div>}
            {footer && <div className="fy-dialog__foot sticky bottom-0">{footer}</div>}
          </Primitive.Popup>
        </Primitive.Viewport>
      </Primitive.Portal>
    </Primitive.Root>
  );
}
