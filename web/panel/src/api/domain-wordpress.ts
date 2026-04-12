export type WordPressDatabase = {
  name: string;
  username: string;
  host: string;
};

export type WordPressCoreUpdate = {
  version?: string;
  update_type?: string;
  package_url?: string;
};

export type WordPressExtension = {
  name: string;
  title?: string;
  status?: string;
  version?: string;
  update?: string;
  update_version?: string;
  auto_update?: string;
};

export type WordPressStatus = {
  cli_available: boolean;
  cli_path?: string;
  document_root: string;
  suggested_database_name?: string;
  config_present: boolean;
  core_files_present: boolean;
  installed: boolean;
  inspect_error?: string;
  version?: string;
  site_url?: string;
  site_title?: string;
  core_update?: WordPressCoreUpdate | null;
  plugins: WordPressExtension[];
  themes: WordPressExtension[];
  databases: WordPressDatabase[];
};

export type WordPressInstallInput = {
  database_name: string;
  site_url: string;
  site_title: string;
  admin_username: string;
  admin_email: string;
  admin_password: string;
  table_prefix: string;
  clear_document_root?: boolean;
};

export type WordPressInstallExtensionInput = {
  slug: string;
  activate: boolean;
};

export type WordPressExtensionActionInput = {
  name: string;
  action: "activate" | "deactivate" | "delete" | "update";
};

export type WordPressApiError = Error & {
  fieldErrors?: Record<string, string>;
};

type WordPressStatusPayload = {
  wordpress: WordPressStatus;
};

export async function fetchDomainWordPressStatus(
  hostname: string,
): Promise<WordPressStatus> {
  const response = await fetch(
    `/api/domains/${encodeURIComponent(hostname)}/wordpress`,
    {
      credentials: "include",
      cache: "no-store",
    },
  );

  return parseWordPressStatusResponse(response, "load wordpress toolkit");
}

export async function installDomainWordPress(
  hostname: string,
  input: WordPressInstallInput,
): Promise<WordPressStatus> {
  const response = await fetch(
    `/api/domains/${encodeURIComponent(hostname)}/wordpress/install`,
    {
      method: "POST",
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(input),
    },
  );

  return parseWordPressStatusResponse(response, "install wordpress");
}

export async function updateDomainWordPressCore(
  hostname: string,
): Promise<WordPressStatus> {
  const response = await fetch(
    `/api/domains/${encodeURIComponent(hostname)}/wordpress/core/update`,
    {
      method: "POST",
      credentials: "include",
    },
  );

  return parseWordPressStatusResponse(response, "update wordpress core");
}

export async function installDomainWordPressPlugin(
  hostname: string,
  input: WordPressInstallExtensionInput,
): Promise<WordPressStatus> {
  const response = await fetch(
    `/api/domains/${encodeURIComponent(hostname)}/wordpress/plugins/install`,
    {
      method: "POST",
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(input),
    },
  );

  return parseWordPressStatusResponse(response, "install wordpress plugin");
}

export async function runDomainWordPressPluginAction(
  hostname: string,
  input: WordPressExtensionActionInput,
): Promise<WordPressStatus> {
  const response = await fetch(
    `/api/domains/${encodeURIComponent(hostname)}/wordpress/plugins/action`,
    {
      method: "POST",
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(input),
    },
  );

  return parseWordPressStatusResponse(response, `wordpress plugin ${input.action}`);
}

export async function installDomainWordPressTheme(
  hostname: string,
  input: WordPressInstallExtensionInput,
): Promise<WordPressStatus> {
  const response = await fetch(
    `/api/domains/${encodeURIComponent(hostname)}/wordpress/themes/install`,
    {
      method: "POST",
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(input),
    },
  );

  return parseWordPressStatusResponse(response, "install wordpress theme");
}

export async function runDomainWordPressThemeAction(
  hostname: string,
  input: WordPressExtensionActionInput,
): Promise<WordPressStatus> {
  const response = await fetch(
    `/api/domains/${encodeURIComponent(hostname)}/wordpress/themes/action`,
    {
      method: "POST",
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(input),
    },
  );

  return parseWordPressStatusResponse(response, `wordpress theme ${input.action}`);
}

async function parseWordPressStatusResponse(
  response: Response,
  action: string,
): Promise<WordPressStatus> {
  if (!response.ok) {
    throw await readWordPressApiError(response, action);
  }

  const payload = (await response.json()) as WordPressStatusPayload;
  return payload.wordpress;
}

async function readWordPressApiError(
  response: Response,
  action: string,
): Promise<WordPressApiError> {
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

  const error = new Error(message) as WordPressApiError;
  error.fieldErrors = fieldErrors;
  return error;
}
