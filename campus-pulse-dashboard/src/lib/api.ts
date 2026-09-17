import { API_BASE_URL, USE_SAMPLE_DATA } from "@/config";
import { endSession, getSession } from "@/lib/auth";

export type Stats = {
  rooms: { total: number; occupied: number; busy: number; overloaded: number };
  alerts: { open: number; critical: number; warning: number };
  service_requests: { open: number; urgent: number; escalated: number };
  energy_today: {
    total_kwh: number;
    top_building: { building: string; kwh: number } | null;
  };
  ingestion: { events_last_hour: number; events_per_minute: number };
};

export type RoomStatus = "empty" | "normal" | "busy" | "overloaded";

export type Room = {
  room_id: string;
  name: string;
  building: string;
  type: string;
  capacity: number;
  occupancy: number;
  utilization: number;
  status: RoomStatus;
  temperature: number | null;
  humidity: number | null;
};

// The Cognito ID token from sign-in. Without a valid session, go to the sign-in page.
function sessionToken(): string {
  const session = getSession();
  if (!session) {
    endSession();
    throw new Error("Please sign in");
  }
  return session.idToken;
}

async function authedGet<T>(path: string): Promise<T> {
  const res = await fetch(`${API_BASE_URL}${path}`, {
    headers: { Authorization: `Bearer ${sessionToken()}` },
  });
  if (res.status === 401) {
    endSession();
    throw new Error("Your session has expired");
  }
  if (!res.ok) throw new Error(`Request to ${path} failed (${res.status})`);
  return (await res.json()) as T;
}

const SAMPLE_STATS: Stats = {
  rooms: { total: 8, occupied: 6, busy: 2, overloaded: 1 },
  alerts: { open: 5, critical: 2, warning: 3 },
  service_requests: { open: 7, urgent: 3, escalated: 1 },
  energy_today: {
    total_kwh: 1790,
    top_building: { building: "Engineering-B", kwh: 640 },
  },
  ingestion: { events_last_hour: 3240, events_per_minute: 54 },
};

const SAMPLE_ROOMS: Room[] = [
  { room_id: "lib-a-101", name: "Reading Hall 101", building: "Library-A", type: "Study Hall", capacity: 120, occupancy: 118, utilization: 0.98, status: "busy", temperature: 22.4, humidity: 41 },
  { room_id: "lib-a-204", name: "Group Study 204", building: "Library-A", type: "Study Room", capacity: 8, occupancy: 0, utilization: 0, status: "empty", temperature: 21.1, humidity: 38 },
  { room_id: "eng-b-110", name: "Lecture Hall B110", building: "Engineering-B", type: "Lecture Hall", capacity: 180, occupancy: 214, utilization: 1.19, status: "overloaded", temperature: 26.8, humidity: 55 },
  { room_id: "eng-b-220", name: "Robotics Lab 220", building: "Engineering-B", type: "Lab", capacity: 30, occupancy: 17, utilization: 0.57, status: "normal", temperature: 23.2, humidity: 44 },
  { room_id: "sci-c-305", name: "Chemistry Lab 305", building: "Science-C", type: "Lab", capacity: 24, occupancy: 12, utilization: 0.5, status: "normal", temperature: 21.9, humidity: 47 },
  { room_id: "sci-c-112", name: "Auditorium C112", building: "Science-C", type: "Auditorium", capacity: 150, occupancy: 141, utilization: 0.94, status: "busy", temperature: 24.6, humidity: 52 },
  { room_id: "hub-d-001", name: "Commons D001", building: "StudentHub-D", type: "Common Area", capacity: 200, occupancy: 88, utilization: 0.44, status: "normal", temperature: 22.8, humidity: 43 },
  { room_id: "hub-d-210", name: "Meeting Room 210", building: "StudentHub-D", type: "Meeting Room", capacity: 12, occupancy: 0, utilization: 0, status: "empty", temperature: null, humidity: null },
];

export const fetchStats = (): Promise<Stats> =>
  USE_SAMPLE_DATA ? Promise.resolve(SAMPLE_STATS) : authedGet<Stats>("/stats");
export const fetchRooms = (): Promise<{ items: Room[] }> =>
  USE_SAMPLE_DATA
    ? Promise.resolve({ items: SAMPLE_ROOMS })
    : authedGet<{ items: Room[] }>("/rooms");

export const STATUS_ORDER: Record<RoomStatus, number> = {
  overloaded: 0,
  busy: 1,
  normal: 2,
  empty: 3,
};

export type AlertSeverity = "warning" | "critical";
export type AlertStatus = "open" | "acknowledged" | "resolved";

export type Alert = {
  alert_id: string;
  building: string;
  room: string | null;
  type: string;
  severity: AlertSeverity;
  message: string;
  status: AlertStatus;
  created_at: string;
};

export type RequestPriority = "low" | "medium" | "high" | "urgent";
export type RequestStatus = "open" | "in_progress" | "resolved";

export type ServiceRequest = {
  request_id: string;
  title: string;
  category: string;
  building: string;
  room: string | null;
  priority: RequestPriority;
  status: RequestStatus;
  escalated: boolean;
  escalation_reason: string | null;
  created_by: { name: string };
  created_at: string;
  due_at: string | null;
};

export const PRIORITY_ORDER: Record<RequestPriority, number> = {
  urgent: 0,
  high: 1,
  medium: 2,
  low: 3,
};

async function authedPatch<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(`${API_BASE_URL}${path}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${sessionToken()}` },
    body: JSON.stringify(body),
  });
  if (res.status === 401) {
    endSession();
    throw new Error("Your session has expired");
  }
  if (!res.ok) {
    let message = `Request to ${path} failed (${res.status})`;
    try {
      const data = (await res.json()) as { error?: { message?: string }; message?: string };
      message = data?.error?.message ?? data?.message ?? message;
    } catch {
      /* ignore non-JSON error bodies */
    }
    throw new Error(message);
  }
  return (await res.json().catch(() => ({}))) as T;
}

const minutesAgo = (m: number) => new Date(Date.now() - m * 60_000).toISOString();
const minutesAhead = (m: number) => new Date(Date.now() + m * 60_000).toISOString();

const SAMPLE_ALERTS: Alert[] = [
  { alert_id: "al-1", building: "Engineering-B", room: "B110", type: "overcrowding", severity: "critical", message: "Lecture Hall B110 is 119% over capacity.", status: "open", created_at: minutesAgo(6) },
  { alert_id: "al-2", building: "Science-C", room: "C305", type: "air_quality", severity: "critical", message: "CO2 above 1800 ppm in Chemistry Lab 305.", status: "open", created_at: minutesAgo(41) },
  { alert_id: "al-3", building: "Library-A", room: "101", type: "temperature", severity: "warning", message: "Reading Hall 101 holding 26.1 °C for 30 minutes.", status: "open", created_at: minutesAgo(18) },
  { alert_id: "al-4", building: "StudentHub-D", room: null, type: "energy", severity: "warning", message: "Evening energy draw 22% above weekly baseline.", status: "acknowledged", created_at: minutesAgo(95) },
  { alert_id: "al-5", building: "Engineering-B", room: "B220", type: "sensor_offline", severity: "warning", message: "Humidity sensor in Robotics Lab 220 stopped reporting.", status: "acknowledged", created_at: minutesAgo(150) },
  { alert_id: "al-6", building: "Science-C", room: "C112", type: "temperature", severity: "critical", message: "Auditorium C112 HVAC fault cleared by facilities.", status: "resolved", created_at: minutesAgo(320) },
];

const SAMPLE_REQUESTS: ServiceRequest[] = [
  { request_id: "sr-1", title: "Projector will not power on", category: "AV", building: "Engineering-B", room: "B110", priority: "urgent", status: "open", escalated: true, escalation_reason: "Blocking a 200-seat lecture starting in 30 minutes.", created_by: { name: "Dana Whitfield" }, created_at: minutesAgo(52), due_at: minutesAhead(25) },
  { request_id: "sr-2", title: "Fume hood airflow below threshold", category: "Facilities", building: "Science-C", room: "C305", priority: "urgent", status: "open", escalated: false, escalation_reason: null, created_by: { name: "Priya Raman" }, created_at: minutesAgo(180), due_at: minutesAgo(20) },
  { request_id: "sr-3", title: "Broken chairs in reading hall", category: "Furniture", building: "Library-A", room: "101", priority: "high", status: "in_progress", escalated: false, escalation_reason: null, created_by: { name: "Marcus Lee" }, created_at: minutesAgo(400), due_at: minutesAhead(240) },
  { request_id: "sr-4", title: "Wi-Fi drops in commons", category: "IT", building: "StudentHub-D", room: "D001", priority: "medium", status: "open", escalated: false, escalation_reason: null, created_by: { name: "Alina Roth" }, created_at: minutesAgo(620), due_at: minutesAhead(1200) },
  { request_id: "sr-5", title: "Replace whiteboard markers", category: "Supplies", building: "StudentHub-D", room: "210", priority: "low", status: "open", escalated: false, escalation_reason: null, created_by: { name: "Tom Okafor" }, created_at: minutesAgo(1500), due_at: null },
  { request_id: "sr-6", title: "Door badge reader repaired", category: "Security", building: "Engineering-B", room: "B220", priority: "high", status: "resolved", escalated: false, escalation_reason: null, created_by: { name: "Sofia Brenner" }, created_at: minutesAgo(2400), due_at: minutesAgo(900) },
];

export const fetchAlerts = (status: AlertStatus): Promise<{ items: Alert[] }> =>
  USE_SAMPLE_DATA
    ? Promise.resolve({ items: SAMPLE_ALERTS.filter((a) => a.status === status) })
    : authedGet<{ items: Alert[] }>(`/alerts?status=${status}`);

export const fetchServiceRequests = (
  statuses: RequestStatus[],
): Promise<{ items: ServiceRequest[] }> =>
  USE_SAMPLE_DATA
    ? Promise.resolve({ items: SAMPLE_REQUESTS.filter((r) => statuses.includes(r.status)) })
    : authedGet<{ items: ServiceRequest[] }>(`/service-requests?status=${statuses.join(",")}`);

export async function updateAlert(alertId: string, status: AlertStatus): Promise<void> {
  if (USE_SAMPLE_DATA) {
    const found = SAMPLE_ALERTS.find((a) => a.alert_id === alertId);
    if (found) found.status = status;
    return;
  }
  await authedPatch(`/alerts/${alertId}`, { status });
}

export async function updateServiceRequest(
  requestId: string,
  status: RequestStatus,
): Promise<void> {
  if (USE_SAMPLE_DATA) {
    const found = SAMPLE_REQUESTS.find((r) => r.request_id === requestId);
    if (found) found.status = status;
    return;
  }
  await authedPatch(`/service-requests/${requestId}`, { status });
}

export type BuildingEnergy = {
  building: string;
  total_kwh: number;
  share: number;
  peak_hour: number;
  peak_kwh: number;
  hourly_kwh: number[];
};

export type EnergyStats = {
  date: string;
  total_kwh: number;
  buildings: BuildingEnergy[];
};

function sampleEnergy(date: string): EnergyStats {
  const bases: [string, number][] = [
    ["Engineering-B", 55],
    ["Science-C", 35],
    ["Library-A", 18],
    ["StudentHub-D", 14],
  ];
  const buildings = bases.map(([building, base]) => {
    const hourly_kwh = Array.from({ length: 24 }, (_, h) => {
      const daytime = h >= 7 && h < 20 ? 1 : 0.45;
      return Math.round(base * daytime * (1 + 0.3 * Math.sin(((h - 7) / 13) * Math.PI)) * 10) / 10;
    });
    const total_kwh = Math.round(hourly_kwh.reduce((a, b) => a + b, 0) * 10) / 10;
    const peak_kwh = Math.max(...hourly_kwh);
    return { building, total_kwh, share: 0, peak_hour: hourly_kwh.indexOf(peak_kwh), peak_kwh, hourly_kwh };
  });
  const total_kwh = Math.round(buildings.reduce((a, b) => a + b.total_kwh, 0) * 10) / 10;
  buildings.forEach((b) => (b.share = Math.round((b.total_kwh / total_kwh) * 1000) / 1000));
  return { date, total_kwh, buildings };
}

export const fetchEnergy = (date: string): Promise<EnergyStats> =>
  USE_SAMPLE_DATA
    ? Promise.resolve(sampleEnergy(date))
    : authedGet<EnergyStats>(`/stats/energy?date=${encodeURIComponent(date)}`);
