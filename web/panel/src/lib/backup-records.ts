import type { BackupRecord } from "@/api/backups";

const backupArchiveExtensions = [
  ".zip", ".tar.gz", ".tgz", ".gz", ".gzip",
  ".tar.zst", ".tar.zstd", ".tzst", ".zst", ".zstd",
];
export const backupArchiveAccept = `${backupArchiveExtensions.join(",")},application/zip,application/gzip,application/x-gzip,application/zstd,application/octet-stream`;

export function isBackupArchiveFileName(name: string) {
  const normalizedName = name.trim().toLowerCase();
  return backupArchiveExtensions.some((extension) => normalizedName.endsWith(extension));
}

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
