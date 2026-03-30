import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center rounded-[8px] border px-2 py-1 text-[12px] font-medium leading-none",
  {
    variants: {
      variant: {
        neutral:
          "border-[var(--app-border)] bg-[var(--app-surface-muted)] text-[var(--app-text-muted)]",
        success:
          "border-[var(--app-ok)]/30 bg-[var(--app-ok-soft)] text-[var(--app-ok)]",
        warning:
          "border-[var(--app-warning)]/30 bg-[var(--app-warning-soft)] text-[var(--app-warning)]",
        danger:
          "border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] text-[var(--app-danger)]",
        accent:
          "border-[var(--app-accent)]/30 bg-[var(--app-accent-soft)] text-[#73a8ef]",
      },
    },
    defaultVariants: {
      variant: "neutral",
    },
  },
);

type BadgeProps = React.HTMLAttributes<HTMLDivElement> &
  VariantProps<typeof badgeVariants>;

export function Badge({ className, variant, ...props }: BadgeProps) {
  return <div className={cn(badgeVariants({ variant }), className)} {...props} />;
}
