import { Badge } from "@/components/ui/badge";

type StatusBadgeProps = {
  status: string;
};

export function StatusBadge({ status }: StatusBadgeProps) {
  const normalized = status.toLowerCase();

  if (["healthy", "active", "managed", "verified", "ready", "connected", "idle"].includes(normalized)) {
    return <Badge>{status}</Badge>;
  }

  if (["warning", "pending", "waiting", "skeleton mode", "running"].includes(normalized)) {
    return <Badge variant="secondary">{status}</Badge>;
  }

  if (["error", "failed", "disabled", "mismatch"].includes(normalized)) {
    return <Badge variant="destructive">{status}</Badge>;
  }

  return <Badge variant="outline">{status}</Badge>;
}
