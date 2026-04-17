export type FFmpegStatus = {
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
  remove_available: boolean;
  remove_label?: string;
};

type FFmpegStatusPayload = {
  ffmpeg: FFmpegStatus;
};

async function parseFFmpegResponse(response: Response): Promise<FFmpegStatus> {
  if (!response.ok) {
    let message = `ffmpeg request failed with status ${response.status}`;

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

  const payload = (await response.json()) as FFmpegStatusPayload;
  return payload.ffmpeg;
}

export async function fetchFFmpegStatus(): Promise<FFmpegStatus> {
  const response = await fetch("/api/ffmpeg", {
    credentials: "include",
    cache: "no-store",
  });

  return parseFFmpegResponse(response);
}

export async function installFFmpeg(): Promise<FFmpegStatus> {
  const response = await fetch("/api/ffmpeg/install", {
    method: "POST",
    credentials: "include",
  });

  return parseFFmpegResponse(response);
}

export async function removeFFmpeg(): Promise<FFmpegStatus> {
  const response = await fetch("/api/ffmpeg/remove", {
    method: "POST",
    credentials: "include",
    keepalive: true,
  });

  return parseFFmpegResponse(response);
}
