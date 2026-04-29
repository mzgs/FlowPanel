import type { ComponentType } from "react";
import { Check, LoaderCircle } from "@/components/icons/lucide-icons";
import { cn } from "@/lib/utils";

type IconProps = {
  className?: string;
  size?: number | string;
};

type ActionFeedbackIconProps = {
  busy?: boolean;
  done?: boolean;
  icon: ComponentType<IconProps>;
  className?: string;
  doneClassName?: string;
  size?: number | string;
};

export function ActionFeedbackIcon({
  busy = false,
  done = false,
  icon: Icon,
  className,
  doneClassName = "text-emerald-500",
  size,
}: ActionFeedbackIconProps) {
  if (busy) {
    return <LoaderCircle className={cn(className, "animate-spin")} size={size} />;
  }

  if (done) {
    return <Check className={cn(className, doneClassName)} size={size} />;
  }

  return <Icon className={className} size={size} />;
}
