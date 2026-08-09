import type { DomainKind } from "@/api/domains";

function normalizeFilesystemPath(value: string) {
  return value.trim().replace(/\\/g, "/").replace(/\/+$/, "");
}

function getFileManagerPath(value: string) {
  return normalizeFilesystemPath(value)
    .replace(/^[a-z]:/i, "")
    .replace(/^\/+/, "");
}

function usesManagedHostnamePath(kind: DomainKind) {
  return kind === "Node.js" || kind === "Python" || kind === "App" || kind === "Reverse proxy";
}

export function getFilesPathFromDomainTarget(
  kind: DomainKind,
  hostname: string,
  sitesBasePath: string,
  target: string,
) {
  const normalizedHostname = hostname.trim().toLowerCase().replace(/\.$/, "");
  if (!normalizedHostname) {
    return null;
  }

  const normalizedBasePath = normalizeFilesystemPath(sitesBasePath);
  const normalizedTargetPath = usesManagedHostnamePath(kind)
    ? `${normalizedBasePath}/${normalizedHostname}`
    : normalizeFilesystemPath(target);

  if (!normalizedBasePath || !normalizedTargetPath) {
    return null;
  }

  if (normalizedTargetPath === normalizedBasePath) {
    return getFileManagerPath(normalizedTargetPath);
  }

  const prefix = `${normalizedBasePath}/`;
  if (!normalizedTargetPath.startsWith(prefix)) {
    return null;
  }

  return getFileManagerPath(normalizedTargetPath);
}

export function getDocumentRootDisplayPath(
  kind: DomainKind,
  hostname: string,
  sitesBasePath: string,
  target: string,
) {
  if (!usesManagedHostnamePath(kind)) {
    return target.trim();
  }

  const normalizedBasePath = sitesBasePath.trim().replace(/[\\/]+$/, "");
  const normalizedHostname = hostname.trim().toLowerCase().replace(/\.$/, "");
  if (!normalizedBasePath) {
    return normalizedHostname;
  }
  if (!normalizedHostname) {
    return normalizedBasePath;
  }

  return `${normalizedBasePath}/${normalizedHostname}`;
}
