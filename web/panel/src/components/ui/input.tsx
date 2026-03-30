import * as React from "react";
import { cn } from "@/lib/utils";

const Input = React.forwardRef<HTMLInputElement, React.ComponentProps<"input">>(
  ({ className, ...props }, ref) => {
    return (
      <input
        ref={ref}
        className={cn(
          "flex h-9 w-full rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] px-3 text-[14px] text-[var(--app-text)] outline-none transition-colors duration-150 placeholder:text-[var(--app-text-muted)] focus:border-[var(--app-border-strong)] focus:ring-2 focus:ring-[var(--app-accent)]/20",
          className,
        )}
        {...props}
      />
    );
  },
);

Input.displayName = "Input";

export { Input };
