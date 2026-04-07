import { type PHPSettings } from "@/api/php";

export type DomainKind = "Static site" | "Php site" | "App" | "Reverse proxy";

export type DomainRecord = {
  id: string;
  hostname: string;
  kind: DomainKind;
  target: string;
  php_settings: PHPSettings;
  github_integration?: DomainGitHubIntegration | null;
  cache_enabled: boolean;
  created_at: string;
};

export type DomainGitHubIntegration = {
  repository_url: string;
  auto_deploy_on_push: boolean;
  default_branch: string;
  post_fetch_script: string;
  created_at: string;
  updated_at: string;
};

export type DomainsPayload = {
  sites_base_path: string;
  domains: DomainRecord[];
};

export type CreateDomainInput = {
  hostname: string;
  kind: DomainKind;
  target?: string;
  cache_enabled: boolean;
};

export type UpdateDomainInput = {
  hostname: string;
  kind: DomainKind;
  target?: string;
  cache_enabled: boolean;
};

export type UpdateDomainGitHubIntegrationInput = {
  repository_url: string;
  auto_deploy_on_push: boolean;
  post_fetch_script: string;
};

export type UpdateDomainPHPSettingsInput = {
  max_execution_time: string;
  max_input_time: string;
  memory_limit: string;
  post_max_size: string;
  file_uploads: string;
  upload_max_filesize: string;
  max_file_uploads: string;
  default_socket_timeout: string;
  error_reporting: string;
  display_errors: string;
};

export type CopyDomainWebsiteInput = {
  target_hostname: string;
  replace_target_files: boolean;
};

export type DomainGitHubDeployResult = {
  action: "initialized" | "updated";
};

export type DomainApiError = Error & {
  fieldErrors?: Record<string, string>;
};

export type DeleteDomainInput = {
  deleteDatabase?: boolean;
  deleteDocumentRoot?: boolean;
};

export type DeleteDomainResult = {
  warnings: string[];
};

export async function fetchDomains(): Promise<DomainsPayload> {
  const response = await fetch("/api/domains", {
    credentials: "include",
  });

  if (!response.ok) {
    throw new Error(`domains request failed with status ${response.status}`);
  }

  return response.json();
}

export async function createDomain(input: CreateDomainInput): Promise<DomainRecord> {
  const response = await fetch("/api/domains", {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  return readDomainMutationResponse(response, "create domain");
}

export async function updateDomain(
  id: string,
  input: UpdateDomainInput,
): Promise<DomainRecord> {
  const response = await fetch(`/api/domains/${encodeURIComponent(id)}`, {
    method: "PUT",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  return readDomainMutationResponse(response, "update domain");
}

export async function deleteDomain(
  id: string,
  input: DeleteDomainInput = {},
): Promise<DeleteDomainResult> {
  const params = new URLSearchParams();
  if (input.deleteDatabase) {
    params.set("delete_database", "1");
  }
  if (input.deleteDocumentRoot) {
    params.set("delete_document_root", "1");
  }
  const suffix = params.size ? `?${params.toString()}` : "";

  const response = await fetch(`/api/domains/${encodeURIComponent(id)}${suffix}`, {
    method: "DELETE",
    credentials: "include",
  });

  if (!response.ok) {
    throw await readDomainApiError(response, "delete domain");
  }

  const payload = (await response.json()) as { warnings?: unknown };
  return {
    warnings: Array.isArray(payload.warnings)
      ? payload.warnings.filter((warning): warning is string => typeof warning === "string")
      : [],
  };
}

export async function updateDomainGitHubIntegration(
  hostname: string,
  input: UpdateDomainGitHubIntegrationInput,
): Promise<DomainRecord> {
  const response = await fetch(`/api/domains/${encodeURIComponent(hostname)}/github`, {
    method: "PUT",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  return readDomainMutationResponse(response, "save github integration");
}

export async function updateDomainPHPSettings(
  hostname: string,
  input: UpdateDomainPHPSettingsInput,
): Promise<DomainRecord> {
  const response = await fetch(`/api/domains/${encodeURIComponent(hostname)}/php-settings`, {
    method: "PUT",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  return readDomainMutationResponse(response, "save php settings");
}

export async function deployDomainGitHubIntegration(
  hostname: string,
): Promise<DomainGitHubDeployResult> {
  const response = await fetch(`/api/domains/${encodeURIComponent(hostname)}/github/deploy`, {
    method: "POST",
    credentials: "include",
  });

  if (!response.ok) {
    throw await readDomainApiError(response, "deploy from github");
  }

  const payload = (await response.json()) as { action: DomainGitHubDeployResult["action"] };
  return { action: payload.action };
}

export async function copyDomainWebsite(
  hostname: string,
  input: CopyDomainWebsiteInput,
): Promise<void> {
  const response = await fetch(`/api/domains/${encodeURIComponent(hostname)}/copy`, {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  if (!response.ok) {
    throw await readDomainApiError(response, "copy website");
  }
}

export async function fetchDomainPreview(
  hostname: string,
  options?: {
    refresh?: boolean;
    refreshToken?: number;
    signal?: AbortSignal;
  },
): Promise<Blob> {
  const previewUrl = new URL(getDomainPreviewUrl(hostname), window.location.origin);
  if (options?.refresh) {
    previewUrl.searchParams.set("refresh", "1");
  }
  if (options?.refreshToken) {
    previewUrl.searchParams.set("t", String(options.refreshToken));
  }

  const response = await fetch(previewUrl.pathname + previewUrl.search, {
    credentials: "include",
    signal: options?.signal,
  });

  if (!response.ok) {
    throw await readDomainApiError(response, "load domain preview");
  }

  return response.blob();
}

export function getDomainPreviewUrl(hostname: string): string {
  return `/api/domains/${encodeURIComponent(hostname)}/preview`;
}

export function getDomainSiteUrl(hostname: string): string {
  return `https://${hostname}`;
}

async function readDomainMutationResponse(
  response: Response,
  action: string,
): Promise<DomainRecord> {
  if (!response.ok) {
    throw await readDomainApiError(response, action);
  }

  const payload = (await response.json()) as { domain: DomainRecord };
  return payload.domain;
}

async function readDomainApiError(
  response: Response,
  action: string,
): Promise<DomainApiError> {
  let message = `${action} request failed with status ${response.status}`;
  let fieldErrors: Record<string, string> | undefined;

  try {
    const payload = (await response.json()) as {
      error?: unknown;
      field_errors?: unknown;
    };
    if (typeof payload.error === "string" && payload.error) {
      message = payload.error;
    }
    if (payload.field_errors && typeof payload.field_errors === "object") {
      fieldErrors = payload.field_errors as Record<string, string>;
    }
  } catch {
    // Keep the default message when the response is not valid JSON.
  }

  const error = new Error(message) as DomainApiError;
  error.fieldErrors = fieldErrors;
  return error;
}
