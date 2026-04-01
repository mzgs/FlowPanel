export type CronJob = {
  id: string;
  name: string;
  schedule: string;
  command: string;
  created_at: string;
};

export type CronPayload = {
  enabled: boolean;
  started: boolean;
  jobs: CronJob[];
};

export type CreateCronJobInput = {
  name: string;
  schedule: string;
  command: string;
};

export type UpdateCronJobInput = {
  name: string;
  schedule: string;
  command: string;
};

export type CronApiError = Error & {
  fieldErrors?: Record<string, string>;
};

export async function fetchCronJobs(): Promise<CronPayload> {
  const response = await fetch("/api/cron", {
    credentials: "include",
  });

  if (!response.ok) {
    throw new Error(`cron request failed with status ${response.status}`);
  }

  return response.json();
}

export async function createCronJob(input: CreateCronJobInput): Promise<CronJob> {
  const response = await fetch("/api/cron", {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  if (!response.ok) {
    throw await readCronApiError(response, "create cron job");
  }

  const payload = (await response.json()) as { job: CronJob };
  return payload.job;
}

export async function updateCronJob(id: string, input: UpdateCronJobInput): Promise<CronJob> {
  const response = await fetch(`/api/cron/${encodeURIComponent(id)}`, {
    method: "PUT",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });

  if (!response.ok) {
    throw await readCronApiError(response, "update cron job");
  }

  const payload = (await response.json()) as { job: CronJob };
  return payload.job;
}

export async function runCronJob(id: string): Promise<CronJob> {
  const response = await fetch(`/api/cron/${encodeURIComponent(id)}/run`, {
    method: "POST",
    credentials: "include",
  });

  if (!response.ok) {
    throw await readCronApiError(response, "run cron job");
  }

  const payload = (await response.json()) as { job: CronJob };
  return payload.job;
}

export async function deleteCronJob(id: string): Promise<void> {
  const response = await fetch(`/api/cron/${encodeURIComponent(id)}`, {
    method: "DELETE",
    credentials: "include",
  });

  if (!response.ok) {
    throw await readCronApiError(response, "delete cron job");
  }
}

async function readCronApiError(response: Response, action: string): Promise<CronApiError> {
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

  const error = new Error(message) as CronApiError;
  error.fieldErrors = fieldErrors;
  return error;
}
