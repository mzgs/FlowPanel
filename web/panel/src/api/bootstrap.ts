export type BootstrapPayload = {
  name: string;
  status: string;
  environment: string;
  admin_listen_addr: string;
  phpmyadmin_addr: string;
  cron_enabled: boolean;
};

export async function fetchBootstrap(): Promise<BootstrapPayload> {
  const response = await fetch("/api/bootstrap", {
    credentials: "include",
  });

  if (!response.ok) {
    throw new Error(`bootstrap request failed with status ${response.status}`);
  }

  return response.json();
}
