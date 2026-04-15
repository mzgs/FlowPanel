import type { DomainKind } from "@/api/domains";

function normalizeFilesystemPath(value: string) {
  return value.trim().replace(/\\/g, "/").replace(/\/+$/, "");
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

  if (kind === "Reverse proxy") {
    return normalizedHostname;
  }

  const normalizedBasePath = normalizeFilesystemPath(sitesBasePath);
  const normalizedTargetPath = normalizeFilesystemPath(target);

  if (!normalizedBasePath || !normalizedTargetPath) {
    return null;
  }

  if (normalizedTargetPath === normalizedBasePath) {
    return "";
  }

  const prefix = `${normalizedBasePath}/`;
  if (!normalizedTargetPath.startsWith(prefix)) {
    return null;
  }

  return normalizedTargetPath.slice(prefix.length);
}

export function getDocumentRootDisplayPath(
  kind: DomainKind,
  hostname: string,
  sitesBasePath: string,
  target: string,
) {
  if (kind === "Static site" || kind === "Php site") {
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
