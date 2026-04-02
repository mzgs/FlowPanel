export type ActivityEvent = {
  id: string;
  actor: string;
  category: string;
  action: string;
  resource_type: string;
  resource_id: string;
  resource_label: string;
  status: string;
  message: string;
  created_at: string;
};

type EventsPayload = {
  events: ActivityEvent[];
};

export async function fetchEvents(limit = 100): Promise<EventsPayload> {
  const response = await fetch(`/api/events?limit=${encodeURIComponent(String(limit))}`, {
    credentials: "include",
  });

  if (!response.ok) {
    throw new Error(`events request failed with status ${response.status}`);
  }

  return response.json();
}
