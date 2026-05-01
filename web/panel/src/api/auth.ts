export type AuthUser = {
  id: string;
  username: string;
};

export type AuthSession = {
  authenticated: boolean;
  setup_required: boolean;
  user?: AuthUser;
};

export type AuthCredentials = {
  username: string;
  password: string;
};

export type AuthApiError = Error & {
  fieldErrors?: Record<string, string>;
};

export async function fetchAuthSession(): Promise<AuthSession> {
  const response = await fetch("/api/auth/session", {
    credentials: "include",
    cache: "no-store",
  });

  if (!response.ok) {
    throw await readAuthApiError(response, "load auth session");
  }

  return (await response.json()) as AuthSession;
}

export async function login(input: AuthCredentials): Promise<AuthSession> {
  return submitAuthRequest("/api/auth/login", input, "sign in");
}

export async function logout(): Promise<AuthSession> {
  const response = await fetch("/api/auth/logout", {
    method: "POST",
    credentials: "include",
  });

  if (!response.ok) {
    throw await readAuthApiError(response, "sign out");
  }

  return (await response.json()) as AuthSession;
}

async function submitAuthRequest(
  path: string,
  input: AuthCredentials,
  action: string,
): Promise<AuthSession> {
  const response = await fetch(path, {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  if (!response.ok) {
    throw await readAuthApiError(response, action);
  }

  return (await response.json()) as AuthSession;
}

async function readAuthApiError(
  response: Response,
  action: string,
): Promise<AuthApiError> {
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
    // Keep the default message when the response body is not valid JSON.
  }

  const error = new Error(message) as AuthApiError;
  error.fieldErrors = fieldErrors;
  return error;
}
