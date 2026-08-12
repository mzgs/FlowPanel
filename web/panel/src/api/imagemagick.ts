export type ImageMagickStatus = {
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

async function requestImageMagick(action?: "install" | "update" | "remove") {
  const response = await fetch(`/api/imagemagick${action ? `/${action}` : ""}`, {
    method: action ? "POST" : "GET",
    credentials: "include",
    cache: "no-store",
    keepalive: action === "remove",
  });

  if (!response.ok) {
    let message = `ImageMagick request failed with status ${response.status}`;
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

  return ((await response.json()) as { imagemagick: ImageMagickStatus }).imagemagick;
}

export const fetchImageMagickStatus = () => requestImageMagick();
export const installImageMagick = () => requestImageMagick("install");
export const updateImageMagick = () => requestImageMagick("update");
export const removeImageMagick = () => requestImageMagick("remove");
