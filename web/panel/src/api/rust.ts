export type RustStatus = {
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
  latest_version?: string;
  remove_available: boolean;
  remove_label?: string;
};

type RustStatusPayload = { rust: RustStatus };

async function parseRustResponse(response: Response): Promise<RustStatus> {
  if (!response.ok) {
    let message = `rust request failed with status ${response.status}`;

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

  return ((await response.json()) as RustStatusPayload).rust;
}

export async function fetchRustStatus(): Promise<RustStatus> {
  return parseRustResponse(await fetch("/api/rust", { credentials: "include", cache: "no-store" }));
}

export async function installRust(): Promise<RustStatus> {
  return parseRustResponse(await fetch("/api/rust/install", { method: "POST", credentials: "include" }));
}

export async function updateRust(): Promise<RustStatus> {
  return parseRustResponse(await fetch("/api/rust/update", { method: "POST", credentials: "include" }));
}

export async function removeRust(): Promise<RustStatus> {
  return parseRustResponse(
    await fetch("/api/rust/remove", { method: "POST", credentials: "include", keepalive: true }),
  );
}
