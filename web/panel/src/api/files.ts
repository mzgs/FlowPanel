export type FileEntryType = "directory" | "file" | "symlink";

export type FileEntry = {
  name: string;
  path: string;
  type: FileEntryType;
  extension?: string;
  size: number;
  modified_at: string;
};

export type FileListing = {
  root_name: string;
  root_path: string;
  path: string;
  parent_path?: string;
  absolute_path: string;
  directories: FileEntry[];
  files: FileEntry[];
};

export type FileContent = {
  name: string;
  path: string;
  extension?: string;
  size: number;
  modified_at: string;
  content: string;
};

type NamedPathInput = {
  path: string;
  name: string;
};

type SaveFileInput = {
  path: string;
  content: string;
};

type TransferEntriesInput = {
  mode: "copy" | "move";
  paths: string[];
  target: string;
};

type FileApiError = Error;

export async function fetchFiles(path = ""): Promise<FileListing> {
  const response = await fetch(`/api/files?path=${encodeURIComponent(path)}`, {
    credentials: "include",
  });

  if (!response.ok) {
    throw await readFileApiError(response, "load files");
  }

  return response.json();
}

export async function createDirectory(input: NamedPathInput): Promise<void> {
  await sendJSON("/api/files/directories", "POST", input, "create directory");
}

export async function createFile(input: NamedPathInput): Promise<void> {
  await sendJSON("/api/files/documents", "POST", input, "create file");
}

export async function renameEntry(input: NamedPathInput): Promise<string> {
  const response = await sendJSON<{ path: string }>(
    "/api/files/rename",
    "POST",
    input,
    "rename entry",
  );

  return response.path;
}

export async function deleteEntry(path: string): Promise<void> {
  const response = await fetch(`/api/files?path=${encodeURIComponent(path)}`, {
    method: "DELETE",
    credentials: "include",
  });

  if (!response.ok) {
    throw await readFileApiError(response, "delete entry");
  }
}

export async function fetchFileContent(path: string): Promise<FileContent> {
  const response = await fetch(`/api/files/content?path=${encodeURIComponent(path)}`, {
    credentials: "include",
  });

  if (!response.ok) {
    throw await readFileApiError(response, "load file");
  }

  return response.json();
}

export async function saveFileContent(input: SaveFileInput): Promise<void> {
  await sendJSON("/api/files/content", "PUT", input, "save file");
}

export async function uploadFiles(path: string, files: File[]): Promise<void> {
  const formData = new FormData();
  formData.set("path", path);
  for (const file of files) {
    formData.append("files", file);
  }

  const response = await fetch("/api/files/upload", {
    method: "POST",
    credentials: "include",
    body: formData,
  });

  if (!response.ok) {
    throw await readFileApiError(response, "upload files");
  }
}

export function getDownloadUrl(path: string) {
  return `/api/files/download?path=${encodeURIComponent(path)}`;
}

export async function downloadEntry(path: string): Promise<string> {
  const response = await fetch(getDownloadUrl(path), {
    credentials: "include",
  });

  if (!response.ok) {
    throw await readFileApiError(response, "download entry");
  }

  const blob = await response.blob();
  const downloadUrl = window.URL.createObjectURL(blob);
  const fileName = getDownloadFilename(response.headers.get("Content-Disposition"), path);
  const anchor = document.createElement("a");

  anchor.href = downloadUrl;
  anchor.download = fileName;
  anchor.style.display = "none";
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  window.setTimeout(() => {
    window.URL.revokeObjectURL(downloadUrl);
  }, 0);

  return fileName;
}

export async function transferEntries(input: TransferEntriesInput): Promise<void> {
  await sendJSON("/api/files/transfer", "POST", input, "transfer entries");
}

async function sendJSON<T>(
  url: string,
  method: "POST" | "PUT",
  body: object,
  action: string,
): Promise<T> {
  const response = await fetch(url, {
    method,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(body),
  });

  if (!response.ok) {
    throw await readFileApiError(response, action);
  }

  return response.json() as Promise<T>;
}

async function readFileApiError(response: Response, action: string): Promise<FileApiError> {
  let message = `${action} request failed with status ${response.status}`;

  try {
    const payload = (await response.json()) as { error?: unknown };
    if (typeof payload.error === "string" && payload.error) {
      message = payload.error;
    }
  } catch {
    // Ignore non-JSON error responses.
  }

  return new Error(message);
}

function getDownloadFilename(contentDisposition: string | null, path: string) {
  if (contentDisposition) {
    const encodedMatch = contentDisposition.match(/filename\*=UTF-8''([^;]+)/i);
    if (encodedMatch?.[1]) {
      return decodeURIComponent(encodedMatch[1]);
    }

    const plainMatch = contentDisposition.match(/filename=\"([^\"]+)\"|filename=([^;]+)/i);
    const value = plainMatch?.[1] ?? plainMatch?.[2];
    if (value) {
      return value.trim();
    }
  }

  const fallback = path.split("/").filter(Boolean).pop() ?? "download";
  return fallback;
}
