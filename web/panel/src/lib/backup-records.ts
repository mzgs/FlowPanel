import type { BackupRecord } from "@/api/backups";

const siteBackupPrefix = "flowpanel-site-";
const siteBackupSeparator = "-backup";
const databaseBackupPrefix = "flowpanel-database-";
const databaseBackupSeparator = "-backup-";

export function getSiteHostnameFromBackupRecord(record: BackupRecord) {
  if (!record.name.startsWith(siteBackupPrefix)) {
    return null;
  }

  const suffixIndex = record.name.indexOf(
    siteBackupSeparator,
    siteBackupPrefix.length,
  );
  if (suffixIndex <= siteBackupPrefix.length) {
    return null;
  }

  return record.name.slice(siteBackupPrefix.length, suffixIndex);
}

export function getDatabaseNameFromBackupRecord(record: BackupRecord) {
  if (!record.name.startsWith(databaseBackupPrefix)) {
    return null;
  }

  const suffixIndex = record.name.indexOf(
    databaseBackupSeparator,
    databaseBackupPrefix.length,
  );
  if (suffixIndex <= databaseBackupPrefix.length) {
    return null;
  }

  return record.name.slice(databaseBackupPrefix.length, suffixIndex);
}
