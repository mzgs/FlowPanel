import { useState } from "react";
import { downloadMariaDBAllDatabasesBackup, type MariaDBStatus } from "@/api/mariadb";
import { Download, LoaderCircle } from "@/components/icons/tabler-icons";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { getErrorMessage } from "@/lib/utils";
import { toast } from "sonner";

type MariaDBSettingsDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  status: MariaDBStatus | null;
};

function getStatusBadge(status: MariaDBStatus | null) {
  if (!status) {
    return { label: "Unavailable", variant: "outline" as const };
  }

  if (status.ready) {
    return { label: "Ready", variant: "default" as const };
  }
  if (status.service_running) {
    return { label: "Running", variant: "secondary" as const };
  }
  if (status.server_installed || status.client_installed) {
    return { label: "Installed", variant: "outline" as const };
  }

  return { label: "Not installed", variant: "outline" as const };
}

function formatValue(value?: string) {
  const trimmed = value?.trim();
  return trimmed ? trimmed : "Not available";
}

export function MariaDBSettingsDialog({
  open,
  onOpenChange,
  status,
}: MariaDBSettingsDialogProps) {
  const [downloading, setDownloading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const badge = getStatusBadge(status);
  const canDownload = Boolean(status?.ready);

  async function handleDownload() {
    setDownloading(true);
    setError(null);

    try {
      const fileName = await downloadMariaDBAllDatabasesBackup();
      toast.success(`Downloaded ${fileName}.`);
    } catch (downloadError) {
      const message = getErrorMessage(downloadError, "Failed to download MariaDB database archive.");
      setError(message);
      toast.error(message);
    } finally {
      setDownloading(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>MariaDB settings</DialogTitle>
        </DialogHeader>

        <div className="space-y-5">
          {error ? (
            <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-2 text-sm text-[var(--app-danger)]">
              {error}
            </div>
          ) : null}

          <section className="space-y-3">
            <div className="flex items-center gap-2">
              <Badge variant={badge.variant}>{badge.label}</Badge>
              <span className="text-sm text-[var(--app-text-muted)]">{formatValue(status?.product)}</span>
            </div>
            <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3 text-sm text-[var(--app-text)]">
              {formatValue(status?.message)}
            </div>
          </section>

          <section className="grid gap-3 sm:grid-cols-2">
            <div className="rounded-lg border border-[var(--app-border)] px-4 py-3">
              <div className="text-xs text-[var(--app-text-muted)]">Version</div>
              <div className="mt-1 break-all text-sm text-[var(--app-text)]">{formatValue(status?.version)}</div>
            </div>
            <div className="rounded-lg border border-[var(--app-border)] px-4 py-3">
              <div className="text-xs text-[var(--app-text-muted)]">Listener</div>
              <div className="mt-1 break-all text-sm text-[var(--app-text)]">{formatValue(status?.listen_address)}</div>
            </div>
            <div className="rounded-lg border border-[var(--app-border)] px-4 py-3">
              <div className="text-xs text-[var(--app-text-muted)]">Server binary</div>
              <div className="mt-1 break-all text-sm text-[var(--app-text)]">{formatValue(status?.server_path)}</div>
            </div>
            <div className="rounded-lg border border-[var(--app-border)] px-4 py-3">
              <div className="text-xs text-[var(--app-text-muted)]">Client binary</div>
              <div className="mt-1 break-all text-sm text-[var(--app-text)]">{formatValue(status?.client_path)}</div>
            </div>
          </section>

          <section className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-4">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="space-y-1">
                <div className="text-sm font-medium text-[var(--app-text)]">All databases</div>
                <p className="text-xs text-[var(--app-text-muted)]">
                  Download one `.tar.gz` archive with individual SQL dumps for each non-system database.
                </p>
              </div>
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  void handleDownload();
                }}
                disabled={downloading || !canDownload}
                title={canDownload ? undefined : "MariaDB must be running before the database archive can be created."}
              >
                {downloading ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
                Download all databases
              </Button>
            </div>
          </section>
        </div>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={downloading}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
