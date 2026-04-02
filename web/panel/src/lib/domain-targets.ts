import type { DomainKind } from "@/api/domains";

function normalizeFilesystemPath(value: string) {
  return value.trim().replace(/\\/g, "/").replace(/\/+$/, "");
}

export function getFilesPathFromDomainTarget(
  kind: DomainKind,
  sitesBasePath: string,
  target: string,
) {
  if (kind !== "Static site" && kind !== "Php site") {
    return null;
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
