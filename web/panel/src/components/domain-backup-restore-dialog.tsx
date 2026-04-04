import { useEffect, useMemo, useState } from "react";
import type { BackupRecord } from "@/api/backups";
import {
  BackupCreateButton,
  BackupRecordsTable,
} from "@/components/backup-records-dialog";
import { Database, HardDrive } from "@/components/icons/tabler-icons";
import { ActionConfirmDialog } from "@/components/action-confirm-dialog";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";

type DatabaseBackupSection = {
  name: string;
  backups: BackupRecord[];
};

type DomainBackupRestoreDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  hostname: string;
  showSiteBackups: boolean;
  siteBackups: BackupRecord[];
  databaseSections: DatabaseBackupSection[];
  loading: boolean;
  loadError: string | null;
  onCreateSiteBackup: () => void;
  createSiteBackupDisabled: boolean;
  createSiteBackupBusy: boolean;
  createSiteBackupDone: boolean;
  onCreateDatabaseBackup: (name: string) => void;
  createDatabaseBackupDisabled: boolean;
  creatingDatabaseBackupName: string | null;
  createdDatabaseBackupName: string | null;
  onRestoreBackup: (name: string) => void;
  restoringBackupName: string | null;
  restoredBackupName: string | null;
  onDeleteBackup: (name: string) => void;
  deletingBackupName: string | null;
};

export function DomainBackupRestoreDialog({
  open,
  onOpenChange,
  hostname,
  showSiteBackups,
  siteBackups,
  databaseSections,
  loading,
  loadError,
  onCreateSiteBackup,
  createSiteBackupDisabled,
  createSiteBackupBusy,
  createSiteBackupDone,
  onCreateDatabaseBackup,
  createDatabaseBackupDisabled,
  creatingDatabaseBackupName,
  createdDatabaseBackupName,
  onRestoreBackup,
  restoringBackupName,
  restoredBackupName,
  onDeleteBackup,
  deletingBackupName,
}: DomainBackupRestoreDialogProps) {
  const [confirmDeleteBackupName, setConfirmDeleteBackupName] = useState<string | null>(null);
  const [confirmRestoreBackupName, setConfirmRestoreBackupName] = useState<string | null>(null);

  const knownBackupNames = useMemo(
    () =>
      new Set([
        ...siteBackups.map((backup) => backup.name),
        ...databaseSections.flatMap((section) => section.backups.map((backup) => backup.name)),
      ]),
    [databaseSections, siteBackups],
  );

  useEffect(() => {
    if (!open) {
      setConfirmDeleteBackupName(null);
      setConfirmRestoreBackupName(null);
    }
  }, [open]);

  useEffect(() => {
    if (
      confirmDeleteBackupName !== null &&
      deletingBackupName !== confirmDeleteBackupName &&
      !knownBackupNames.has(confirmDeleteBackupName)
    ) {
      setConfirmDeleteBackupName(null);
    }
  }, [confirmDeleteBackupName, deletingBackupName, knownBackupNames]);

  function getRestoreConfirmDescription(name: string) {
    if (siteBackups.some((backup) => backup.name === name)) {
      return `Restore backup "${name}"? This overwrites the site files stored in that archive.`;
    }

    if (
      databaseSections.some((section) =>
        section.backups.some((backup) => backup.name === name),
      )
    ) {
      return `Restore backup "${name}"? This overwrites the database contents stored in that archive.`;
    }

    return `Restore backup "${name}"?`;
  }

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="max-h-[85vh] gap-5 overflow-y-auto sm:max-w-5xl">
          <DialogHeader>
            <DialogTitle>{hostname} backups</DialogTitle>
          </DialogHeader>

          {loadError ? (
            <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-4 text-[13px] text-[var(--app-danger)]">
              {loadError}
            </div>
          ) : null}

          {showSiteBackups ? (
            <section className="space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <h3 className="flex items-center gap-2 text-sm font-semibold text-[var(--app-text)]">
                  <HardDrive className="h-4 w-4 text-[var(--app-text-muted)]" stroke={1.8} />
                  <span>Site backups</span>
                </h3>
                <BackupCreateButton
                  onClick={onCreateSiteBackup}
                  disabled={createSiteBackupDisabled}
                  busy={createSiteBackupBusy}
                  done={createSiteBackupDone}
                />
              </div>
              {loading ? (
                <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-3 py-4 text-[13px] text-[var(--app-text-muted)]">
                  Loading backups...
                </div>
              ) : (
                <BackupRecordsTable
                  backups={siteBackups}
                  onRequestRestore={(name) => {
                    setConfirmRestoreBackupName(name);
                  }}
                  restoringBackupName={restoringBackupName}
                  restoredBackupName={restoredBackupName}
                  onRequestDelete={(name) => {
                    setConfirmDeleteBackupName(name);
                  }}
                  deletingBackupName={deletingBackupName}
                  emptyMessage="No site backups found."
                />
              )}
            </section>
          ) : null}

          <section className="space-y-3">
            <h3 className="flex items-center gap-2 text-sm font-semibold text-[var(--app-text)]">
              <Database className="h-4 w-4 text-[var(--app-text-muted)]" stroke={1.8} />
              <span>Database backups</span>
            </h3>
            {loading ? (
              <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-3 py-4 text-[13px] text-[var(--app-text-muted)]">
                Loading backups...
              </div>
            ) : databaseSections.length === 0 ? (
              <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-3 py-4 text-[13px] text-[var(--app-text-muted)]">
                No databases connected to this domain.
              </div>
            ) : (
              <div className="space-y-4">
                {databaseSections.map((section) => (
                  <section
                    key={section.name}
                    className="space-y-3 rounded-xl border border-[var(--app-border)] bg-[var(--app-surface)] p-4"
                  >
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <h4 className="text-sm font-semibold text-[var(--app-text)]">
                        {section.name}
                      </h4>
                      <BackupCreateButton
                        onClick={() => {
                          onCreateDatabaseBackup(section.name);
                        }}
                        disabled={createDatabaseBackupDisabled}
                        busy={creatingDatabaseBackupName === section.name}
                        done={createdDatabaseBackupName === section.name}
                      />
                    </div>
                    <BackupRecordsTable
                      backups={section.backups}
                      onRequestRestore={(name) => {
                        setConfirmRestoreBackupName(name);
                      }}
                      restoringBackupName={restoringBackupName}
                      restoredBackupName={restoredBackupName}
                      onRequestDelete={(name) => {
                        setConfirmDeleteBackupName(name);
                      }}
                      deletingBackupName={deletingBackupName}
                    />
                  </section>
                ))}
              </div>
            )}
          </section>
        </DialogContent>
      </Dialog>
      <ActionConfirmDialog
        open={confirmRestoreBackupName !== null}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) {
            setConfirmRestoreBackupName(null);
          }
        }}
        title="Restore backup"
        desc={
          confirmRestoreBackupName
            ? getRestoreConfirmDescription(confirmRestoreBackupName)
            : "Restore this backup?"
        }
        confirmText="Restore backup"
        handleConfirm={() => {
          if (confirmRestoreBackupName !== null) {
            onRestoreBackup(confirmRestoreBackupName);
            setConfirmRestoreBackupName(null);
          }
        }}
        className="sm:max-w-md"
      />
      <ActionConfirmDialog
        open={confirmDeleteBackupName !== null}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) {
            setConfirmDeleteBackupName(null);
          }
        }}
        title="Delete backup"
        desc={
          confirmDeleteBackupName
            ? `Delete backup "${confirmDeleteBackupName}"?`
            : "Delete this backup?"
        }
        confirmText="Delete backup"
        destructive
        isLoading={
          confirmDeleteBackupName !== null &&
          deletingBackupName === confirmDeleteBackupName
        }
        handleConfirm={() => {
          if (confirmDeleteBackupName !== null) {
            onDeleteBackup(confirmDeleteBackupName);
            setConfirmDeleteBackupName(null);
          }
        }}
        className="sm:max-w-md"
      />
    </>
  );
}
