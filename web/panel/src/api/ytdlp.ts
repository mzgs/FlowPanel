export type YTDLPStatus = {
  platform: string;
  package_manager?: string;
  installed: boolean;
  binary_path?: string;
  version?: string;
  state: string;
  message: string;
  issues?: string[];
  install_available: boolean;
  install_label?: string;
  update_available: boolean;
  update_label?: string;
  remove_available: boolean;
  remove_label?: string;
};

type YTDLPStatusPayload = {
  ytdlp: YTDLPStatus;
};

async function parseYTDLPResponse(response: Response): Promise<YTDLPStatus> {
  if (!response.ok) {
    let message = `yt-dlp request failed with status ${response.status}`;

    try {
      const payload = await response.json();
      if (typeof payload.error === "string" && payload.error) {
        message = payload.error;
      }
    } catch {
      // Keep the default error message when the payload is not JSON.
    }

    throw new Error(message);
  }

  const payload = (await response.json()) as YTDLPStatusPayload;
  return payload.ytdlp;
}

async function requestYTDLP(action?: "install" | "update" | "remove") {
  const response = await fetch(`/api/ytdlp${action ? `/${action}` : ""}`, {
    method: action ? "POST" : "GET",
    credentials: "include",
    cache: "no-store",
    keepalive: action === "remove",
  });

  return parseYTDLPResponse(response);
}

export function fetchYTDLPStatus() {
  return requestYTDLP();
}

export function installYTDLP() {
  return requestYTDLP("install");
}

export function updateYTDLP() {
  return requestYTDLP("update");
}

export function removeYTDLP() {
  return requestYTDLP("remove");
}
