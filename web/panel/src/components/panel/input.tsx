import * as React from "react";
import { cn } from "@/lib/utils";

export const Input = React.forwardRef<HTMLInputElement, React.ComponentProps<"input">>(
  ({ className, ...props }, ref) => (
    <input
      ref={ref}
      className={cn(
        "block h-10 w-full rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-3 text-[14px] text-[var(--app-text)] shadow-sm outline-none transition-colors placeholder:text-[var(--app-text-muted)] focus:border-[var(--app-accent)] focus:ring-2 focus:ring-blue-100",
        className,
      )}
      {...props}
    />
  ),
);

Input.displayName = "Input";
