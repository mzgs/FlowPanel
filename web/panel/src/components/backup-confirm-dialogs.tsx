import { useEffect, useState } from "react";
import { ActionConfirmDialog } from "@/components/action-confirm-dialog";

type BackupConfirmStateOptions = {
  open: boolean;
  backupNames: ReadonlySet<string>;
  deletingBackupName: string | null;
};

type BackupConfirmDialogsProps = {
  confirmDeleteBackupName: string | null;
  setConfirmDeleteBackupName: (name: string | null) => void;
  confirmRestoreBackupName: string | null;
  setConfirmRestoreBackupName: (name: string | null) => void;
  onRestoreBackup: (name: string) => void;
  onDeleteBackup: (name: string) => void;
  deletingBackupName: string | null;
  closeDeleteOnConfirm?: boolean;
  restoreConfirmTitle?: string;
  restoreConfirmText?: string;
  getRestoreConfirmDescription?: (name: string) => string;
};

export function useBackupConfirmState({
  open,
  backupNames,
  deletingBackupName,
}: BackupConfirmStateOptions) {
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
      !backupNames.has(confirmDeleteBackupName)
    ) {
      setConfirmDeleteBackupName(null);
    }
  }, [backupNames, confirmDeleteBackupName, deletingBackupName]);

  return {
    confirmDeleteBackupName,
    setConfirmDeleteBackupName,
    confirmRestoreBackupName,
    setConfirmRestoreBackupName,
  };
}

export function BackupConfirmDialogs({
  confirmDeleteBackupName,
  setConfirmDeleteBackupName,
  confirmRestoreBackupName,
  setConfirmRestoreBackupName,
  onRestoreBackup,
  onDeleteBackup,
  deletingBackupName,
  closeDeleteOnConfirm = false,
  restoreConfirmTitle = "Restore backup",
  restoreConfirmText = "Restore backup",
  getRestoreConfirmDescription,
}: BackupConfirmDialogsProps) {
  return (
    <>
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
            if (closeDeleteOnConfirm) {
              setConfirmDeleteBackupName(null);
            }
          }
        }}
        className="sm:max-w-md"
      />
    </>
  );
}
