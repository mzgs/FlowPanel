export type DomainKind = "Static site" | "Php site" | "App" | "Reverse proxy";

export type DomainRecord = {
  id: string;
  hostname: string;
  kind: DomainKind;
  target: string;
  cache_enabled: boolean;
  created_at: string;
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

export type DomainApiError = Error & {
  fieldErrors?: Record<string, string>;
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

export async function deleteDomain(id: string): Promise<void> {
  const response = await fetch(`/api/domains/${encodeURIComponent(id)}`, {
    method: "DELETE",
    credentials: "include",
  });

  if (!response.ok) {
    throw await readDomainApiError(response, "delete domain");
  }
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
