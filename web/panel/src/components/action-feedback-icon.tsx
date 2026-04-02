import type { ComponentType } from "react";
import { Check, LoaderCircle } from "@/components/icons/tabler-icons";
import { cn } from "@/lib/utils";

type IconProps = {
  className?: string;
  stroke?: number | string;
  size?: number | string;
};

type ActionFeedbackIconProps = {
  busy?: boolean;
  done?: boolean;
  icon: ComponentType<IconProps>;
  className?: string;
  doneClassName?: string;
  stroke?: number | string;
  size?: number | string;
};

export function ActionFeedbackIcon({
  busy = false,
  done = false,
  icon: Icon,
  className,
  doneClassName = "text-emerald-500",
  stroke,
  size,
}: ActionFeedbackIconProps) {
  if (busy) {
    return <LoaderCircle className={cn(className, "animate-spin")} stroke={stroke} size={size} />;
  }

  if (done) {
    return <Check className={cn(className, doneClassName)} stroke={stroke} size={size} />;
  }

  return <Icon className={className} stroke={stroke} size={size} />;
}
