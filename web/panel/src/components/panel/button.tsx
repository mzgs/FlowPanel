import * as React from "react";
import { cn } from "@/lib/utils";

type ButtonVariant = "default" | "secondary" | "quiet";
type ButtonSize = "default" | "sm";

const variantClasses: Record<ButtonVariant, string> = {
  default:
    "border border-transparent bg-[var(--app-accent)] text-white shadow-sm hover:bg-blue-500 focus-visible:ring-[var(--app-accent)]",
  secondary:
    "border border-[var(--app-border)] bg-[var(--app-surface)] text-[var(--app-text)] shadow-sm hover:bg-[var(--app-surface-muted)] focus-visible:ring-[var(--app-accent)]",
  quiet:
    "border border-transparent bg-transparent text-[var(--app-text-muted)] hover:bg-[var(--app-surface-muted)] hover:text-[var(--app-text)] focus-visible:ring-[var(--app-accent)]",
};

const sizeClasses: Record<ButtonSize, string> = {
  default: "h-10 px-4 text-[14px]",
  sm: "h-9 px-3 text-[13px]",
};

export type ButtonProps = React.ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant;
  size?: ButtonSize;
};

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant = "default", size = "default", type = "button", ...props }, ref) => (
    <button
      ref={ref}
      type={type}
      className={cn(
        "inline-flex items-center justify-center gap-2 rounded-lg font-medium transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--app-surface)] disabled:pointer-events-none disabled:opacity-50",
        variantClasses[variant],
        sizeClasses[size],
        className,
      )}
      {...props}
    />
  ),
);

Button.displayName = "Button";
