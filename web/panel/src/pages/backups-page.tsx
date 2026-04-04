import { useEffect, useRef, useState } from "react";
import {
  createBackup,
  deleteBackup,
  fetchBackups,
  getBackupDownloadUrl,
  importBackup,
  restoreBackup,
  type CreateBackupInput,
  type BackupRecord,
} from "@/api/backups";
import {
  Database,
  Download,
  FolderOpen,
  HardDrive,
  LoaderCircle,
  RotateCcw,
  Trash2,
  Upload,
} from "@/components/icons/tabler-icons";
import { ActionFeedbackIcon } from "@/components/action-feedback-icon";
import { ActionConfirmDialog } from "@/components/action-confirm-dialog";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatBytes, formatDateTime } from "@/lib/format";
import { toast } from "sonner";

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
}

const initialScope: CreateBackupInput = {
  include_panel_data: true,
  include_sites: true,
  include_databases: true,
};
const backupArchiveExtension = ".tar.gz";

function isBackupArchiveFileName(name: string) {
  const normalizedName = name.trim().toLowerCase();
  return normalizedName.endsWith(backupArchiveExtension);
}

export function BackupsPage() {
  const [backups, setBackups] = useState<BackupRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [deletingName, setDeletingName] = useState<string | null>(null);
  const [confirmDeleteName, setConfirmDeleteName] = useState<string | null>(null);
  const [confirmRestoreName, setConfirmRestoreName] = useState<string | null>(null);
  const [restoringName, setRestoringName] = useState<string | null>(null);
  const [restoredName, setRestoredName] = useState<string | null>(null);
  const [importing, setImporting] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [scope, setScope] = useState<CreateBackupInput>(initialScope);
  const restoredTimeoutRef = useRef<number | null>(null);
  const importInputRef = useRef<HTMLInputElement | null>(null);

  const hasSelectedScope =
    scope.include_panel_data || scope.include_sites || scope.include_databases;
  const panelDataCheckboxId = "backup-scope-panel-data";
  const siteFilesCheckboxId = "backup-scope-site-files";
  const databaseDumpsCheckboxId = "backup-scope-database-dumps";

  async function loadBackups() {
    try {
      const payload = await fetchBackups();
      setBackups(payload.backups);
      setLoadError(null);
    } catch (error) {
      setLoadError(getErrorMessage(error, "Failed to load backups."));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void loadBackups();
  }, []);

  useEffect(() => {
    return () => {
      if (restoredTimeoutRef.current !== null) {
        window.clearTimeout(restoredTimeoutRef.current);
      }
    };
  }, []);

  async function handleCreateBackup() {
    if (!hasSelectedScope) {
      toast.error("Select at least one backup source.");
      return;
    }

    setCreating(true);

    try {
      const record = await createBackup(scope);
      setBackups((current) => [record, ...current.filter((item) => item.name !== record.name)]);
      setLoadError(null);
      setCreateDialogOpen(false);
      toast.success(`Created backup ${record.name}.`);
    } catch (error) {
      toast.error(getErrorMessage(error, "Failed to create backup."));
    } finally {
      setCreating(false);
    }
  }

  function handleDeleteBackup(name: string) {
    if (deletingName !== null) {
      return;
    }

    setConfirmDeleteName(name);
  }

  async function confirmDeleteBackup() {
    if (!confirmDeleteName) {
      return;
    }

    const name = confirmDeleteName;

    setDeletingName(name);

    try {
      await deleteBackup(name);
      setBackups((current) => current.filter((item) => item.name !== name));
      toast.success(`Deleted backup ${name}.`);
    } catch (error) {
      toast.error(getErrorMessage(error, "Failed to delete backup."));
    } finally {
      setDeletingName(null);
      setConfirmDeleteName((current) => (current === name ? null : current));
    }
  }

  function handleRestoreBackup(name: string) {
    if (restoringName !== null || deletingName !== null) {
      return;
    }

    setConfirmRestoreName(name);
  }

  async function confirmRestoreBackup() {
    if (!confirmRestoreName) {
      return;
    }

    const name = confirmRestoreName;

    setRestoringName(name);
    setRestoredName(null);

    try {
      const result = await restoreBackup(name);
      if (restoredTimeoutRef.current !== null) {
        window.clearTimeout(restoredTimeoutRef.current);
      }
      setRestoredName(name);
      restoredTimeoutRef.current = window.setTimeout(() => {
        setRestoredName((current) => (current === name ? null : current));
        restoredTimeoutRef.current = null;
      }, 1500);
      if (result.restored_panel_database) {
        window.setTimeout(() => {
          window.location.reload();
        }, 700);
      }
    } catch (error) {
      toast.error(getErrorMessage(error, "Failed to restore backup."));
    } finally {
      setRestoringName(null);
      setConfirmRestoreName((current) => (current === name ? null : current));
    }
  }

  function handleOpenImportDialog() {
    if (importing) {
      return;
    }

    importInputRef.current?.click();
  }

  async function handleImportSelection(files: FileList | null) {
    const file = files?.[0];
    if (!file) {
      return;
    }
    if (!isBackupArchiveFileName(file.name)) {
      toast.error("Select a FlowPanel backup archive ending in .tar.gz.");
      return;
    }

    setImporting(true);

    try {
      const record = await importBackup(file);
      setBackups((current) => [record, ...current.filter((item) => item.name !== record.name)]);
      setLoadError(null);
      toast.success(`Imported backup ${record.name}.`);
    } catch (error) {
      toast.error(getErrorMessage(error, "Failed to import backup."));
    } finally {
      setImporting(false);
    }
  }

  return (
    <>
      <PageHeader
        title="Backups"
        actions={(
          <>
            <Button type="button" variant="outline" onClick={handleOpenImportDialog} disabled={importing}>
              {importing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}
              Import backup
            </Button>
            <Button type="button" onClick={() => setCreateDialogOpen(true)}>
              <HardDrive className="h-4 w-4" />
              Create backup
            </Button>
          </>
        )}
      />

      <input
        ref={importInputRef}
        type="file"
        accept={`${backupArchiveExtension},application/gzip,application/x-gzip,application/octet-stream`}
        className="hidden"
        onChange={(event) => {
          void handleImportSelection(event.target.files);
          event.target.value = "";
        }}
      />

      <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create backup</DialogTitle>
            <DialogDescription>
              Select which FlowPanel-managed sources should be included in the archive.
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-3">
            <label
              htmlFor={panelDataCheckboxId}
              className="flex cursor-pointer items-start gap-3 rounded-lg border border-[var(--app-border)] px-3 py-3"
            >
              <Checkbox
                id={panelDataCheckboxId}
                checked={scope.include_panel_data}
                onCheckedChange={(checked) =>
                  setScope((current) => ({
                    ...current,
                    include_panel_data: checked === true,
                  }))
                }
                className="mt-0.5"
              />
              <div className="min-w-0">
                <Label htmlFor={panelDataCheckboxId} className="cursor-pointer text-sm text-foreground">
                  <HardDrive className="h-4 w-4" />
                  Panel data
                </Label>
                <p className="mt-1 text-sm leading-6 text-muted-foreground">
                  FlowPanel data files, runtime secrets, and the SQLite panel snapshot.
                </p>
              </div>
            </label>

            <label
              htmlFor={siteFilesCheckboxId}
              className="flex cursor-pointer items-start gap-3 rounded-lg border border-[var(--app-border)] px-3 py-3"
            >
              <Checkbox
                id={siteFilesCheckboxId}
                checked={scope.include_sites}
                onCheckedChange={(checked) =>
                  setScope((current) => ({
                    ...current,
                    include_sites: checked === true,
                  }))
                }
                className="mt-0.5"
              />
              <div className="min-w-0">
                <Label htmlFor={siteFilesCheckboxId} className="cursor-pointer text-sm text-foreground">
                  <FolderOpen className="h-4 w-4" />
                  Site files
                </Label>
                <p className="mt-1 text-sm leading-6 text-muted-foreground">
                  Local document roots for static and PHP domains managed by the panel.
                </p>
              </div>
            </label>

            <label
              htmlFor={databaseDumpsCheckboxId}
              className="flex cursor-pointer items-start gap-3 rounded-lg border border-[var(--app-border)] px-3 py-3"
            >
              <Checkbox
                id={databaseDumpsCheckboxId}
                checked={scope.include_databases}
                onCheckedChange={(checked) =>
                  setScope((current) => ({
                    ...current,
                    include_databases: checked === true,
                  }))
                }
                className="mt-0.5"
              />
              <div className="min-w-0">
                <Label htmlFor={databaseDumpsCheckboxId} className="cursor-pointer text-sm text-foreground">
                  <Database className="h-4 w-4" />
                  Database dumps
                </Label>
                <p className="mt-1 text-sm leading-6 text-muted-foreground">
                  SQL exports for MariaDB databases tracked by FlowPanel.
                </p>
              </div>
            </label>
          </div>

          {!hasSelectedScope ? (
            <div className="rounded-lg border border-[var(--app-border)] px-3 py-2 text-sm text-[var(--app-danger)]">
              Select at least one source before creating a backup.
            </div>
          ) : null}

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setCreateDialogOpen(false)}
              disabled={creating}
            >
              Cancel
            </Button>
            <Button
              type="button"
              onClick={() => void handleCreateBackup()}
              disabled={creating || !hasSelectedScope}
            >
              {creating ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <HardDrive className="h-4 w-4" />}
              Create backup
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ActionConfirmDialog
        open={confirmRestoreName !== null}
        onOpenChange={(open) => {
          if (!open && restoringName === null) {
            setConfirmRestoreName(null);
          }
        }}
        title="Restore backup"
        desc={
          confirmRestoreName
            ? `Restore backup "${confirmRestoreName}"? This overwrites the matching panel files, site files, and databases contained in the archive.`
            : "Restore this backup?"
        }
        confirmText="Restore backup"
        isLoading={confirmRestoreName !== null && restoringName === confirmRestoreName}
        handleConfirm={() => {
          void confirmRestoreBackup();
        }}
        className="sm:max-w-md"
      />

      <ActionConfirmDialog
        open={confirmDeleteName !== null}
        onOpenChange={(open) => {
          if (!open && deletingName === null) {
            setConfirmDeleteName(null);
          }
        }}
        title="Delete backup"
        desc={confirmDeleteName ? `Delete backup "${confirmDeleteName}"?` : "Delete this backup?"}
        confirmText="Delete backup"
        destructive
        isLoading={confirmDeleteName !== null && deletingName === confirmDeleteName}
        handleConfirm={() => {
          void confirmDeleteBackup();
        }}
        className="sm:max-w-md"
      />

      <div className="px-4 pb-6 sm:px-6 lg:px-8">
        <section className="overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)]">
          {loadError ? (
            <div className="border-b border-[var(--app-border)] px-4 py-3 text-sm text-[var(--app-danger)]">
              {loadError}
            </div>
          ) : null}

          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead className="w-[180px]">Created</TableHead>
                <TableHead className="w-[120px]">Size</TableHead>
                <TableHead className="w-[220px] text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow>
                  <TableCell colSpan={4} className="h-40 text-center text-sm text-muted-foreground">
                    Loading backups...
                  </TableCell>
                </TableRow>
              ) : backups.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className="h-40 text-center text-sm text-muted-foreground">
                    No backups created yet.
                  </TableCell>
                </TableRow>
              ) : (
                backups.map((backup) => {
                  const deleting = deletingName === backup.name;
                  const restoring = restoringName === backup.name;
                  const restored = restoredName === backup.name;

                  return (
                    <TableRow key={backup.name}>
                      <TableCell className="font-medium text-foreground">{backup.name}</TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatDateTime(backup.created_at)}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatBytes(backup.size)}
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-2">
                          <Button
                            type="button"
                            variant="outline"
                            size="icon"
                            onClick={() => handleRestoreBackup(backup.name)}
                            disabled={restoring || deleting}
                            aria-label={`Restore ${backup.name}`}
                            title={`Restore ${backup.name}`}
                          >
                            <ActionFeedbackIcon
                              busy={restoring}
                              done={restored}
                              icon={RotateCcw}
                              className="h-4 w-4"
                            />
                          </Button>
                          <Button type="button" variant="outline" size="icon" asChild>
                            <a
                              href={getBackupDownloadUrl(backup.name)}
                              aria-label={`Download ${backup.name}`}
                              title={`Download ${backup.name}`}
                            >
                              <Download className="h-4 w-4" />
                            </a>
                          </Button>
                          <Button
                            type="button"
                            variant="destructive"
                            size="icon"
                            onClick={() => handleDeleteBackup(backup.name)}
                            disabled={deleting}
                            aria-label={`Delete ${backup.name}`}
                            title={`Delete ${backup.name}`}
                          >
                            {deleting ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })
              )}
            </TableBody>
          </Table>
        </section>
      </div>
    </>
  );
}
