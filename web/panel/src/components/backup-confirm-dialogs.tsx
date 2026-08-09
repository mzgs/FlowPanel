import { useEffect, useRef, useState } from "react";
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
  restoringBackupName: string | null;
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
  restoringBackupName,
  onDeleteBackup,
  deletingBackupName,
  closeDeleteOnConfirm = false,
  restoreConfirmTitle = "Restore backup",
  restoreConfirmText = "Restore backup",
  getRestoreConfirmDescription,
}: BackupConfirmDialogsProps) {
  const restoreWasLoading = useRef(false);
  const restoreIsLoading =
    confirmRestoreBackupName !== null &&
    restoringBackupName === confirmRestoreBackupName;

  useEffect(() => {
    if (restoreWasLoading.current && !restoreIsLoading) {
      setConfirmRestoreBackupName(null);
    }
    restoreWasLoading.current = restoreIsLoading;
  }, [restoreIsLoading, setConfirmRestoreBackupName]);

  return (
    <>
      <ActionConfirmDialog
        open={confirmRestoreBackupName !== null}
        onOpenChange={(nextOpen) => {
          if (!nextOpen && !restoreIsLoading) {
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
        isLoading={restoreIsLoading}
        handleConfirm={() => {
          if (confirmRestoreBackupName !== null) {
            onRestoreBackup(confirmRestoreBackupName);
          }
        }}
        className="sm:max-w-lg"
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
