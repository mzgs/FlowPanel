import * as React from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";
import { cn } from "@/lib/utils";

type ModalContextValue = {
  open: boolean;
  onOpenChange?: (open: boolean) => void;
};

type SyntheticModalEvent = {
  defaultPrevented: boolean;
  preventDefault: () => void;
};

const ModalContext = React.createContext<ModalContextValue | null>(null);

function createSyntheticEvent(): SyntheticModalEvent {
  return {
    defaultPrevented: false,
    preventDefault() {
      this.defaultPrevented = true;
    },
  };
}

function useModalContext() {
  const context = React.useContext(ModalContext);
  if (!context) {
    throw new Error("Dialog components must be used inside Dialog.");
  }
  return context;
}

type DialogProps = {
  open: boolean;
  onOpenChange?: (open: boolean) => void;
  children: React.ReactNode;
};

export function Dialog({ open, onOpenChange, children }: DialogProps) {
  return <ModalContext.Provider value={{ open, onOpenChange }}>{children}</ModalContext.Provider>;
}

type DialogContentProps = React.HTMLAttributes<HTMLDivElement> & {
  onEscapeKeyDown?: (event: SyntheticModalEvent) => void;
  onPointerDownOutside?: (event: SyntheticModalEvent) => void;
  onOpenAutoFocus?: (event: SyntheticModalEvent) => void;
};

export const DialogContent = React.forwardRef<HTMLDivElement, DialogContentProps>(
  (
    {
      children,
      className,
      onEscapeKeyDown,
      onPointerDownOutside,
      onOpenAutoFocus,
      ...props
    },
    ref,
  ) => {
    const { open, onOpenChange } = useModalContext();
    const localRef = React.useRef<HTMLDivElement | null>(null);

    React.useImperativeHandle(ref, () => localRef.current as HTMLDivElement);

    React.useEffect(() => {
      if (!open) {
        return;
      }

      const previousOverflow = document.body.style.overflow;
      document.body.style.overflow = "hidden";

      const autoFocusEvent = createSyntheticEvent();
      onOpenAutoFocus?.(autoFocusEvent);
      if (!autoFocusEvent.defaultPrevented) {
        requestAnimationFrame(() => {
          const element = localRef.current;
          if (!element) {
            return;
          }

          const focusTarget = element.querySelector<HTMLElement>(
            "button, [href], input, select, textarea, [tabindex]:not([tabindex='-1'])",
          );
          (focusTarget ?? element).focus();
        });
      }

      function handleKeyDown(event: KeyboardEvent) {
        if (event.key !== "Escape") {
          return;
        }

        const escapeEvent = createSyntheticEvent();
        onEscapeKeyDown?.(escapeEvent);
        if (!escapeEvent.defaultPrevented) {
          onOpenChange?.(false);
        }
      }

      document.addEventListener("keydown", handleKeyDown);
      return () => {
        document.body.style.overflow = previousOverflow;
        document.removeEventListener("keydown", handleKeyDown);
      };
    }, [onEscapeKeyDown, onOpenAutoFocus, onOpenChange, open]);

    if (!open || typeof document === "undefined") {
      return null;
    }

    return createPortal(
      <div
        className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 px-4 py-6 backdrop-blur-[2px]"
        onMouseDown={(event) => {
          if (event.target !== event.currentTarget) {
            return;
          }

          const outsideEvent = createSyntheticEvent();
          onPointerDownOutside?.(outsideEvent);
          if (!outsideEvent.defaultPrevented) {
            onOpenChange?.(false);
          }
        }}
      >
        <div
          ref={localRef}
          tabIndex={-1}
          className={cn(
            "relative grid w-[min(42rem,calc(100vw-2rem))] max-h-[calc(100vh-2rem)] gap-4 overflow-y-auto rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface)] p-6 shadow-[0_20px_45px_rgba(0,0,0,0.45)] outline-none",
            className,
          )}
          {...props}
        >
          {children}
          <button
            type="button"
            onClick={() => onOpenChange?.(false)}
            className="absolute right-4 top-4 inline-flex h-9 w-9 items-center justify-center rounded-lg text-[var(--app-text-muted)] transition-colors hover:bg-[var(--app-surface-muted)] hover:text-[var(--app-text)]"
          >
            <X className="h-4 w-4" />
            <span className="sr-only">Close</span>
          </button>
        </div>
      </div>,
      document.body,
    );
  },
);

DialogContent.displayName = "DialogContent";

export function DialogHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("space-y-1 pr-10", className)} {...props} />;
}

export function DialogFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn("flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between", className)}
      {...props}
    />
  );
}

export const DialogTitle = React.forwardRef<HTMLHeadingElement, React.HTMLAttributes<HTMLHeadingElement>>(
  ({ className, ...props }, ref) => (
    <h2
      ref={ref}
      className={cn("text-[22px] font-semibold tracking-[-0.03em] text-[var(--app-text)]", className)}
      {...props}
    />
  ),
);

DialogTitle.displayName = "DialogTitle";

export const DialogDescription = React.forwardRef<
  HTMLParagraphElement,
  React.HTMLAttributes<HTMLParagraphElement>
>(({ className, ...props }, ref) => (
  <p ref={ref} className={cn("text-[14px] leading-6 text-[var(--app-text-muted)]", className)} {...props} />
));

DialogDescription.displayName = "DialogDescription";
