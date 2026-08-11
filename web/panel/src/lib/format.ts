const dateTimeFormatter = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  hour: "2-digit",
  minute: "2-digit",
});

export function formatDateTime(value: string) {
  return dateTimeFormatter.format(new Date(value));
}

export function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) {
    return "0 B";
  }

  const units = ["B", "KB", "MB", "GB", "TB"];
  const exponent = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  const size = value / 1024 ** exponent;

  return `${size >= 10 || exponent === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[exponent]}`;
}

export function formatUploadTimeRemaining(
  loaded: number,
  total: number,
  startedAt: number,
) {
  const elapsedSeconds = (Date.now() - startedAt) / 1000;
  if (loaded <= 0 || total <= loaded || elapsedSeconds < 1) {
    return null;
  }

  const seconds = Math.ceil((total - loaded) / (loaded / elapsedSeconds));
  if (!Number.isFinite(seconds)) {
    return null;
  }
  if (seconds < 60) {
    return `${seconds}s left`;
  }
  if (seconds < 3600) {
    return `${Math.ceil(seconds / 60)}m left`;
  }

  const hours = Math.floor(seconds / 3600);
  return `${hours}h ${Math.ceil((seconds % 3600) / 60)}m left`;
}
