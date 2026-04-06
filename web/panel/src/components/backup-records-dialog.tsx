import { useEffect, useState } from "react";
import {
  Download,
  HardDrive,
  LoaderCircle,
  RotateCcw,
  Trash2,
} from "@/components/icons/tabler-icons";
import { getBackupDownloadUrl, type BackupRecord } from "@/api/backups";
import { ActionFeedbackIcon } from "@/components/action-feedback-icon";
import { ActionConfirmDialog } from "@/components/action-confirm-dialog";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatBytes, formatDateTime } from "@/lib/format";

type BackupCreateButtonProps = {
  onClick: () => void;
  disabled: boolean;
  busy: boolean;
  done: boolean;
};

type BackupRecordsTableProps = {
  backups: BackupRecord[];
  onRequestRestore: (name: string) => void;
  restoringBackupName: string | null;
  restoredBackupName: string | null;
  onRequestDelete: (name: string) => void;
  deletingBackupName: string | null;
  emptyMessage?: string;
  actionIconStroke?: number;
};

type BackupRecordsDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  backups: BackupRecord[];
  onCreateBackup: () => void;
  createDisabled: boolean;
  createBusy: boolean;
  createDone: boolean;
  onRestoreBackup: (name: string) => void;
  restoringBackupName: string | null;
  restoredBackupName: string | null;
  restoreConfirmTitle?: string;
  restoreConfirmText?: string;
  getRestoreConfirmDescription?: (name: string) => string;
  onDeleteBackup: (name: string) => void;
  deletingBackupName: string | null;
  actionIconStroke?: number;
};

export function BackupCreateButton({
  onClick,
  disabled,
  busy,
  done,
}: BackupCreateButtonProps) {
  return (
    <Button type="button" size="sm" onClick={onClick} disabled={disabled}>
      {busy ? (
        <LoaderCircle className="h-4 w-4 animate-spin" />
      ) : (
        <HardDrive className="h-4 w-4" />
      )}
      {busy ? "Backing up..." : done ? "Backup created" : "Backup now"}
    </Button>
  );
}

export function BackupRecordsTable({
  backups,
  onRequestRestore,
  restoringBackupName,
  restoredBackupName,
  onRequestDelete,
  deletingBackupName,
  emptyMessage = "No backups found.",
  actionIconStroke,
}: BackupRecordsTableProps) {
  if (backups.length === 0) {
    return (
      <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-3 py-4 text-[13px] text-[var(--app-text-muted)]">
        {emptyMessage}
      </div>
    );
  }

  return (
    <div className="overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)]">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead>Backup name</TableHead>
            <TableHead>Date</TableHead>
            <TableHead>Size</TableHead>
            <TableHead className="text-right">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {backups.map((backup) => (
            <TableRow key={backup.name}>
              <TableCell
                className="max-w-[280px] truncate font-medium text-[var(--app-text)]"
                title={backup.name}
              >
                {backup.name}
              </TableCell>
              <TableCell className="text-[13px] text-[var(--app-text-muted)]">
                {formatDateTime(backup.created_at)}
              </TableCell>
              <TableCell className="text-[13px] text-[var(--app-text-muted)]">
                {formatBytes(backup.size)}
              </TableCell>
              <TableCell className="text-right">
                <div className="flex items-center justify-end gap-1">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    onClick={() => {
                      onRequestRestore(backup.name);
                    }}
                    disabled={
                      restoringBackupName === backup.name ||
                      deletingBackupName === backup.name
                    }
                    aria-label={`Restore ${backup.name}`}
                    title={`Restore ${backup.name}`}
                  >
                    <ActionFeedbackIcon
                      busy={restoringBackupName === backup.name}
                      done={restoredBackupName === backup.name}
                      icon={RotateCcw}
                      className="size-6"
                      stroke={actionIconStroke}
                    />
                  </Button>
                  <Button
                    asChild
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={`Download ${backup.name}`}
                    title={`Download ${backup.name}`}
                  >
                    <a href={getBackupDownloadUrl(backup.id, backup.location)}>
                      <Download className="size-6" stroke={actionIconStroke} />
                    </a>
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    onClick={() => {
                      onRequestDelete(backup.name);
                    }}
                    disabled={
                      deletingBackupName === backup.name ||
                      restoringBackupName === backup.name
                    }
                    aria-label={`Delete ${backup.name}`}
                    title={`Delete ${backup.name}`}
                  >
                    {deletingBackupName === backup.name ? (
                      <LoaderCircle
                        className="size-6 animate-spin"
                        stroke={actionIconStroke}
                      />
                    ) : (
                      <Trash2 className="size-6" stroke={actionIconStroke} />
                    )}
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

export function BackupRecordsDialog({
  open,
  onOpenChange,
  title,
  backups,
  onCreateBackup,
  createDisabled,
  createBusy,
  createDone,
  onRestoreBackup,
  restoringBackupName,
  restoredBackupName,
  restoreConfirmTitle = "Restore backup",
  restoreConfirmText = "Restore backup",
  getRestoreConfirmDescription,
  onDeleteBackup,
  deletingBackupName,
  actionIconStroke,
}: BackupRecordsDialogProps) {
  const [confirmDeleteBackupName, setConfirmDeleteBackupName] = useState<string | null>(null);
  const [confirmRestoreBackupName, setConfirmRestoreBackupName] = useState<string | null>(null);

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
      !backups.some((backup) => backup.name === confirmDeleteBackupName)
    ) {
      setConfirmDeleteBackupName(null);
    }
  }, [backups, confirmDeleteBackupName, deletingBackupName]);

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="gap-4 sm:max-w-3xl">
          <DialogHeader>
            <div className="flex flex-wrap items-center gap-3">
              <DialogTitle>{title}</DialogTitle>
              <BackupCreateButton
                onClick={onCreateBackup}
                disabled={createDisabled}
                busy={createBusy}
                done={createDone}
              />
            </div>
          </DialogHeader>
          <BackupRecordsTable
            backups={backups}
            onRequestRestore={(name) => {
              setConfirmRestoreBackupName(name);
            }}
            restoringBackupName={restoringBackupName}
            restoredBackupName={restoredBackupName}
            onRequestDelete={(name) => {
              setConfirmDeleteBackupName(name);
            }}
            deletingBackupName={deletingBackupName}
            actionIconStroke={actionIconStroke}
          />
        </DialogContent>
      </Dialog>
      <ActionConfirmDialog
        open={confirmRestoreBackupName !== null}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) {
            setConfirmRestoreBackupName(null);
          }
        }}
        title={restoreConfirmTitle}
        desc={
          confirmRestoreBackupName
            ? (getRestoreConfirmDescription?.(confirmRestoreBackupName) ??
              `Restore backup "${confirmRestoreBackupName}"?`)
            : "Restore this backup?"
        }
        confirmText={restoreConfirmText}
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
        desc={confirmDeleteBackupName ? `Delete backup "${confirmDeleteBackupName}"?` : "Delete this backup?"}
        confirmText="Delete backup"
        destructive
        isLoading={
          confirmDeleteBackupName !== null &&
          deletingBackupName === confirmDeleteBackupName
        }
        handleConfirm={() => {
          if (confirmDeleteBackupName !== null) {
            onDeleteBackup(confirmDeleteBackupName);
          }
        }}
        className="sm:max-w-md"
      />
    </>
  );
}
