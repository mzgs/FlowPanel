import type { PHPRuntimeStatus } from "@/api/php";

export function getPHPActionLabel(state?: string | null) {
  switch (state) {
    case "installing":
      return "Installing...";
    case "removing":
      return "Removing...";
    case "starting":
      return "Starting...";
    case "stopping":
      return "Stopping...";
    case "restarting":
      return "Restarting...";
    default:
      return null;
  }
}

export function isPHPActionState(state?: string | null) {
  return getPHPActionLabel(state) !== null;
}

export function formatPHPVersion(status: PHPRuntimeStatus | null) {
  const actionLabel = getPHPActionLabel(status?.state);
  if (actionLabel) {
    return actionLabel;
  }

  if (!status?.php_installed) {
    return "Not installed";
  }

  const version = status.php_version?.trim();
  if (!version) {
    return "Installed";
  }

  const match = version.match(/\bPHP\s+(\d+(?:\.\d+)+)\b/i);
  return match?.[1] ?? version;
}

export function getPHPServiceLabel(status: PHPRuntimeStatus | null) {
  if (!status) {
    return "Unavailable";
  }

  const actionLabel = getPHPActionLabel(status.state);
  if (actionLabel) {
    return actionLabel.replace("...", "");
  }

  if (status.service_running) {
    return "Running";
  }

  if (status.fpm_installed) {
    return "Installed";
  }

  return "Not installed";
}
