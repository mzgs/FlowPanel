export type FirewallPort = {
  port: number;
  end_port?: number;
  protocol: "tcp" | "udp";
  source: string;
};

export type FirewallStatus = {
  supported: boolean;
  enabled: boolean;
  active: boolean;
  backend?: string;
  allowed: FirewallPort[];
  docker_ports: FirewallPort[];
  notice?: string;
};

type FirewallPayload = {
  firewall: FirewallStatus;
};

async function requestFirewall(path: string, init?: RequestInit) {
  const response = await fetch(path, {
    credentials: "include",
    cache: "no-store",
    ...init,
  });
  if (!response.ok) {
    let message = `Firewall request failed with status ${response.status}.`;
    try {
      const payload = (await response.json()) as { error?: string };
      if (payload.error) {
        message = payload.error;
      }
    } catch {
      // Keep the status-based error.
    }
    throw new Error(message);
  }
  return ((await response.json()) as FirewallPayload).firewall;
}

export function fetchFirewall() {
  return requestFirewall("/api/firewall");
}

export function setFirewallEnabled(enabled: boolean) {
  return requestFirewall("/api/firewall", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ enabled }),
  });
}

export function reconcileFirewall() {
  return requestFirewall("/api/firewall/reconcile", { method: "POST" });
}

export function updateFirewallPort(port: Omit<FirewallPort, "source">, open: boolean) {
  return requestFirewall("/api/firewall/ports", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      port: port.port,
      end_port: port.end_port,
      protocol: port.protocol,
      open,
    }),
  });
}
