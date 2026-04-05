const PENDING_FILES_PATH_STORAGE_KEY = "flowpanel.files.pending-path";

function normalizePath(value: string) {
  return value.trim();
}

export function setPendingFilesPath(path: string | null) {
  if (typeof window === "undefined") {
    return;
  }

  if (path !== null) {
    window.sessionStorage.setItem(
      PENDING_FILES_PATH_STORAGE_KEY,
      normalizePath(path),
    );
    return;
  }

  window.sessionStorage.removeItem(PENDING_FILES_PATH_STORAGE_KEY);
}

export function consumePendingFilesPath() {
  if (typeof window === "undefined") {
    return null;
  }

  const value = window.sessionStorage.getItem(PENDING_FILES_PATH_STORAGE_KEY);
  window.sessionStorage.removeItem(PENDING_FILES_PATH_STORAGE_KEY);

  if (value === null) {
    return null;
  }

  return normalizePath(value);
}
