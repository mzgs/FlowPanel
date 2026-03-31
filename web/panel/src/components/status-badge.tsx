import { Badge } from "@/components/panel/badge";

type StatusBadgeProps = {
  status: string;
};

export function StatusBadge({ status }: StatusBadgeProps) {
  const normalized = status.toLowerCase();

  if (["healthy", "active", "managed", "verified", "ready", "connected", "idle"].includes(normalized)) {
    return <Badge variant="success">{status}</Badge>;
  }

  if (["warning", "pending", "waiting", "skeleton mode", "running"].includes(normalized)) {
    return <Badge variant="warning">{status}</Badge>;
  }

  if (["error", "failed", "disabled", "mismatch"].includes(normalized)) {
    return <Badge variant="danger">{status}</Badge>;
  }

  return <Badge variant="neutral">{status}</Badge>;
}
