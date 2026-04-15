export type NodeJSStatus = {
  platform: string;
  package_manager?: string;
  installed: boolean;
  binary_path?: string;
  npm_path?: string;
  version?: string;
  state: string;
  message: string;
  issues?: string[];
  install_available: boolean;
  install_label?: string;
  remove_available: boolean;
  remove_label?: string;
};

type NodeJSStatusPayload = {
  nodejs: NodeJSStatus;
};

async function parseNodeJSResponse(response: Response): Promise<NodeJSStatus> {
  if (!response.ok) {
    let message = `nodejs request failed with status ${response.status}`;

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

  const payload = (await response.json()) as NodeJSStatusPayload;
  return payload.nodejs;
}

export async function fetchNodeJSStatus(): Promise<NodeJSStatus> {
  const response = await fetch("/api/nodejs", {
    credentials: "include",
    cache: "no-store",
  });

  return parseNodeJSResponse(response);
}

export async function installNodeJS(): Promise<NodeJSStatus> {
  const response = await fetch("/api/nodejs/install", {
    method: "POST",
    credentials: "include",
  });

  return parseNodeJSResponse(response);
}

export async function removeNodeJS(): Promise<NodeJSStatus> {
  const response = await fetch("/api/nodejs/remove", {
    method: "POST",
    credentials: "include",
  });

  return parseNodeJSResponse(response);
}
