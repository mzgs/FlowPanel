import type { HTMLAttributes } from "react";
import { cn } from "@/lib/utils";

type BadgeVariant = "neutral" | "success" | "warning" | "danger" | "accent";

const badgeClasses: Record<BadgeVariant, string> = {
  neutral: "border-[var(--app-border)] bg-[var(--app-surface-muted)] text-[var(--app-text-muted)]",
  success: "border-green-900/60 bg-green-950/80 text-green-300",
  warning: "border-amber-900/60 bg-amber-950/80 text-amber-300",
  danger: "border-red-900/60 bg-red-950/80 text-red-300",
  accent: "border-blue-900/60 bg-blue-950/80 text-blue-300",
};

type BadgeProps = HTMLAttributes<HTMLDivElement> & {
  variant?: BadgeVariant;
};

export function Badge({ className, variant = "neutral", ...props }: BadgeProps) {
  return (
    <div
      className={cn(
        "inline-flex items-center rounded-full border px-2.5 py-1 text-[12px] font-medium leading-none",
        badgeClasses[variant],
        className,
      )}
      {...props}
    />
  );
}
