import { Package } from "@/components/icons/tabler-icons";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export type ComposerPackage = {
  name: string;
  version: string;
  dev: boolean;
};

type DomainComposerDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  hostname: string;
  projectPath: string;
  hasManifest: boolean;
  packages: ComposerPackage[];
  loading: boolean;
  loadError: string | null;
  runningAction: "install" | "update" | null;
  onInstall: () => void;
  onUpdate: () => void;
};

export function DomainComposerDialog({
  open,
  onOpenChange,
  hostname,
  projectPath,
  hasManifest,
  packages,
  loading,
  loadError,
  runningAction,
  onInstall,
  onUpdate,
}: DomainComposerDialogProps) {
  const canRunActions = hasManifest && runningAction === null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="gap-4 sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{hostname} Composer</DialogTitle>
          <DialogDescription>
            Run Composer commands for this domain and inspect the current dependency set.
          </DialogDescription>
        </DialogHeader>

        {loadError ? (
          <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-4 text-[13px] text-[var(--app-danger)]">
            {loadError}
          </div>
        ) : null}

        <section className="space-y-3 rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="space-y-1">
              <p className="text-sm font-semibold text-[var(--app-text)]">Project</p>
              <p className="break-all font-mono text-xs text-[var(--app-text-muted)]">
                {projectPath || "Loading project path..."}
              </p>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                size="sm"
                onClick={onInstall}
                disabled={!canRunActions}
              >
                {runningAction === "install" ? "Installing..." : "composer install"}
              </Button>
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={onUpdate}
                disabled={!canRunActions}
              >
                {runningAction === "update" ? "Updating..." : "composer update"}
              </Button>
            </div>
          </div>

          {!hasManifest ? (
            <p className="text-[13px] text-[var(--app-text-muted)]">
              No <code>composer.json</code> file was found in this project.
            </p>
          ) : null}
        </section>

        <section className="space-y-3">
          <div className="flex items-center justify-between gap-3">
            <h3 className="flex items-center gap-2 text-sm font-semibold text-[var(--app-text)]">
              <Package className="h-4 w-4 text-[var(--app-text-muted)]" stroke={1.8} />
              <span>Libraries</span>
            </h3>
            <span className="text-xs text-[var(--app-text-muted)]">
              {hasManifest ? "Direct dependencies from composer.json" : "No Composer files found"}
            </span>
          </div>

          {loading ? (
            <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-3 py-4 text-[13px] text-[var(--app-text-muted)]">
              Loading Composer details...
            </div>
          ) : packages.length > 0 ? (
            <div className="overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)]">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead>Library</TableHead>
                    <TableHead className="w-[180px]">Version</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {packages.map((pkg) => (
                    <TableRow key={`${pkg.name}:${pkg.dev ? "dev" : "prod"}`}>
                      <TableCell className="max-w-0">
                        <div className="flex min-w-0 items-center gap-2">
                          <span
                            className="truncate font-medium text-[var(--app-text)]"
                            title={pkg.name}
                          >
                            {pkg.name}
                          </span>
                          {pkg.dev ? <Badge variant="outline">dev</Badge> : null}
                        </div>
                      </TableCell>
                      <TableCell className="font-mono text-[13px] text-[var(--app-text-muted)]">
                        {pkg.version}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          ) : (
            <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-3 py-4 text-[13px] text-[var(--app-text-muted)]">
              {hasManifest ? "No Composer libraries were detected." : "No Composer project found for this domain."}
            </div>
          )}
        </section>
      </DialogContent>
    </Dialog>
  );
}
