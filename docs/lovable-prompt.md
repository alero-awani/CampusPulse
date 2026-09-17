# CampusPulse 2026 — Operations Dashboard

Build a single-page operations dashboard for **CampusPulse 2026**, the smart campus platform of the fictional **NorthBridge University**. It shows data from simulated campus sensors (occupancy, energy, temperature, humidity, doors, equipment) and student service requests, and helps the campus operations team make decisions at a glance.

The backend is an **external REST API written in Go**, fully specified at the end of this prompt. You are building the frontend only.

## Hard rules

- Do **not** use Supabase, Lovable Cloud, or any other backend, database, or auth provider. All data and login go through the REST API below.
- Stack: React + TypeScript + Vite, Tailwind CSS, shadcn/ui, TanStack Query for data fetching, Recharts for charts, date-fns for dates, lucide-react for icons.
- One route (`/`). Signed out: login screen. Signed in: the dashboard for the user's role. Details and forms open in side sheets or dialogs, never new pages.
- Implement every section in this prompt completely. No placeholders, no TODOs, no "coming soon".

## Configuration

- `VITE_API_BASE_URL`: base URL of the API, e.g. `http://localhost:8080` or an AWS API Gateway URL. Never hardcode URLs anywhere else.
- `VITE_USE_MOCKS`: when `"true"`, or when `VITE_API_BASE_URL` is empty, use the built-in mock API (see **Mock mode**). Show a small "Mock data" badge in the header whenever mocks are active.

## API client rules

- Put all types in `src/types/api.ts` exactly as defined in the **Types** section, and all API calls in `src/lib/api.ts`. Components never call `fetch` directly.
- Send `Authorization: Bearer <token>` on every request except `POST /auth/login` and `GET /health`.
- Store the session (`token`, `expires_at`, `user`) in `sessionStorage`. Never log the token.
- `401`, or a token past `expires_at`: clear the session and show the login screen with "Your session has expired. Please sign in again."
- `403`: show "You don't have access to this" inside the affected section. Never crash the page.
- Errors use the `ApiError` shape. For mutations, show `error.message` in a toast. For queries, show it inline in the section with a Retry button.
- URL-encode every ID used in a path.
- Timestamps are ISO 8601 UTC. Display them in the browser's local time: relative for recent times ("4 min ago") with the exact date and time in a tooltip.
- Endpoints that return `Page<T>` get a "Load more" button that requests `cursor=<next_cursor>`. Hide the button when `next_cursor` is `null`.

## Auto-refresh (keep API calls low)

Every API call counts against a cloud free tier, so refresh deliberately:

- Header control: refresh interval **Off / 15s / 30s / 60s**, default **30s**, plus a "Refresh now" button and "Updated X ago" text.
- Use TanStack Query `refetchInterval` for the dashboard sections only. Do not refetch while the browser tab is hidden.
- Detail sheets fetch when opened and when "Refresh now" is clicked; they don't poll.
- `GET /health` runs every 60 seconds for the API status indicator.

## Roles

The API returns `user.role` as `student`, `staff`, or `admin`. The UI adapts:

- **student**: "Find a study space" and "My service requests" only.
- **staff**: the full operations dashboard.
- **admin**: the full operations dashboard, plus an "Alert rules" button in the header.

## Login screen

Centered card: CampusPulse wordmark, "NorthBridge University Operations", email and password fields, Sign in button with loading state. Calls `POST /auth/login`. On `invalid_credentials`, show "Incorrect email or password" under the form. In mock mode, list the demo accounts below the form.

## Staff and admin dashboard

### Header (sticky)

- Left: "CampusPulse" wordmark and "NorthBridge University".
- Section links that smooth-scroll to each section: Overview · Rooms · Energy · Alerts · Requests · Live feed.
- **Building filter**: "All buildings" plus every building found in `GET /rooms`. It applies to every section whose endpoint accepts `building`.
- Refresh control (see Auto-refresh).
- API status dot from `GET /health`: green "API online" or red "API unreachable".
- Admin only: "Alert rules" button.
- User menu: name, role badge, light/dark/system theme, Sign out.

### 1. Overview — `GET /stats`

A row of KPI cards (wraps on small screens):

| Card | Main value | Sub-line | Highlight |
|---|---|---|---|
| Rooms occupied | `rooms.occupied / rooms.total` | "`rooms.busy` busy" | — |
| Overloaded rooms | `rooms.overloaded` | "over capacity now" | red when > 0 |
| Open alerts | `alerts.open` | "`critical` critical · `warning` warning" | red when critical > 0 |
| Urgent requests | `service_requests.urgent` | "`escalated` escalated · `open` open" | red when escalated > 0 |
| Energy today | `energy_today.total_kwh` kWh | "Top: `top_building.building` (`kwh` kWh)" | — |
| Events last hour | `ingestion.events_last_hour` | "`events_per_minute`/min" | gray "No data" when 0 |

Clicking a card scrolls to its section and applies the matching filter (e.g. Overloaded rooms scrolls to Rooms filtered to `overloaded`; Urgent requests scrolls to Requests filtered to `urgent`).

### 2. Rooms — "Which rooms are occupied or overloaded?" — `GET /rooms`

- Status filter chips with counts: All, Overloaded, Busy, Normal, Empty. Room type select.
- Rooms grouped by building. Each room is a compact card showing: name, type, `occupancy / capacity` people, a utilization bar (visually capped at 100% but labeled with the real percentage, e.g. "105%"), status badge, temperature and humidity when not null, and an alert icon with the count when `open_alerts > 0`.
- Sort within each building: overloaded, busy, normal, empty; then by `utilization` descending.
- Clicking a card opens a side sheet loaded from `GET /rooms/{roomId}?hours=6` with an hours selector (1 / 6 / 24):
  - current values at the top
  - line chart of occupancy with a dashed reference line at capacity
  - line chart of temperature (°C, left axis) and humidity (%, right axis)
  - table of `recent_events`

### 3. Energy — "Which buildings consume the most energy during the day?" — `GET /stats/energy?date=`

- Date picker, default today (local date, `YYYY-MM-DD`). Day total in large text.
- Horizontal bar chart of buildings ranked by `total_kwh`, each bar labeled with kWh and share %. When a building filter is active, highlight that building instead of hiding the others.
- Line chart of `hourly_kwh` (hours 0–23), one line per building, with a legend.
- Table: building, total kWh, share, peak hour (e.g. "14:00–15:00"), peak kWh.

### 4. Alerts — "Are there unusual temperature, humidity, or equipment events?" — `GET /alerts`

- Filters: status (default `open`), severity, type.
- Table columns: severity badge, type (with icon), building / room, message, value + unit vs threshold, created (relative), status.
- Sort: critical first, then newest. Critical rows get a subtle red left border.
- Row actions calling `PATCH /alerts/{alertId}`: "Acknowledge" (when `open`) and "Resolve" (when `open` or `acknowledged`), each with an optional note in a small popover. Update the row optimistically, roll back on error, then refetch `/stats`.

### 5. Service requests — "Which requests are urgent and need escalation?" — `GET /service-requests`

- Filters: status (default "Active" = `status=open,in_progress`; also Resolved and All), priority, "Escalated only" toggle. Request `limit=100`.
- Sort the loaded list: escalated first, then priority (urgent > high > medium > low), then oldest first.
- Each row: priority badge, "Escalated" badge (tooltip shows `escalation_reason`), title, category, building / room, created by, age, "Overdue" badge when `due_at` has passed and status is not `resolved`, assigned to.
- Clicking a row opens a side sheet loaded from `GET /service-requests/{requestId}` with the full description, a history timeline, and staff actions: status select, "Assign to" text field, "Escalate" switch, note textarea, and Save (`PATCH /service-requests/{requestId}`, sending only changed fields). Refetch the list and `/stats` after saving.

### 6. Live event feed — `GET /events?limit=50`

- Compact table of the latest events: time, building / room, type icon, value + unit, severity badge, device ID.
- Type filter. Refreshes on the global interval. Rows that are new since the previous refresh briefly highlight.

### Alert rules (admin only) — `GET /thresholds`, `PUT /thresholds`

Side sheet opened from the header, with a form in groups:

- **Occupancy**: busy ratio and overloaded ratio, edited as percentages.
- **Temperature (°C)** and **Humidity (%)**: warning min/max and critical min/max.
- **Energy**: warning and critical kWh per hour.
- **Service requests**: urgent categories (multi-select) and "escalate after N minutes".

Validate that values are numbers and that `critical_min ≤ warning_min < warning_max ≤ critical_max`. Show "Last changed by `updated_by`, `updated_at`". Save sends `PUT /thresholds`, then shows a toast.

## Student dashboard

Header: wordmark, building filter, refresh control, user menu. No section links, no API status, no Alert rules.

### Find a study space — `GET /rooms`

- Type filter defaults to `library` and `study_space`, with a toggle to show all room types.
- Cards sorted by free seats (`capacity - occupancy`, highest first), overloaded rooms last. Show free seats prominently ("18 seats free"), status badge, and temperature. Do not show alert counts.

### My service requests — `GET /service-requests`

- "New request" button opens a dialog: category select, building select, room select (rooms of the chosen building, optional), title (required, max 80 characters), description (required, max 1000 characters). Submit sends `POST /service-requests`, then shows the toast "Request submitted — priority: `priority`". The student never chooses the priority; the server sets it.
- List of the student's requests: title, category, status, priority, created, last updated. Clicking one opens a read-only sheet with details and the history timeline.

## Visual design

- Calm, clean operations-console look, readable at a glance on a laptop during a live demo. Light and dark mode.
- Use one color language everywhere:
  - Room status: overloaded = red, busy = amber, normal = green, empty = gray.
  - Severity: critical = red, warning = amber, normal = slate.
  - Priority: urgent = red, high = orange, medium = blue, low = gray.
- Never rely on color alone: every colored badge also has a text label.
- Skeleton loaders while loading, friendly empty states (e.g. "No open alerts"), inline errors with Retry.
- Responsive: sections stack on narrow screens, and tables scroll horizontally inside their card.

## Mock mode

Create `src/lib/mockApi.ts` with the same functions and types as `src/lib/api.ts`, backed by in-memory data with about 300 ms of simulated latency, so the entire UI works without the backend.

- Demo accounts, all with password `demo1234`:
  - `student@northbridge.edu` (Sam Rivera, student)
  - `staff@northbridge.edu` (Jordan Lee, staff)
  - `admin@northbridge.edu` (Alex Morgan, admin)
  - A wrong password returns `401` with code `invalid_credentials`.
- 4 buildings: `Library-A`, `Engineering-B`, `Science-C`, `StudentHub-D`, with about 24 rooms of mixed types, including 2 overloaded and 3 empty.
- Energy: 24 hourly values per building for today that peak around midday, with `Engineering-B` the highest.
- About 12 alerts (mixed severities, types, and statuses), about 10 service requests (2 escalated, 1 overdue), and 50 recent events. Each refresh adds a few new events and slightly changes room occupancy.
- Mutations (`PATCH /alerts/{id}`, `POST` and `PATCH /service-requests`, `PUT /thresholds`) update the in-memory data so the UI reacts.
- Enforce roles like the real API: a student calling a staff endpoint gets `403` with code `forbidden`, and students only see their own service requests.

## API contract

Base URL: `VITE_API_BASE_URL`. All requests and responses are JSON.

| Method | Path | Roles | Query / body | Response |
|---|---|---|---|---|
| GET | `/health` | public | — | `Health` |
| POST | `/auth/login` | public | body `{ email, password }` | `LoginResponse` |
| GET | `/me` | all | — | `User` |
| GET | `/stats` | staff, admin | `building?` | `Stats` |
| GET | `/stats/energy` | staff, admin | `date` (`YYYY-MM-DD`) | `EnergyStats` |
| GET | `/rooms` | all | `building?`, `status?`, `type?` | `{ items: Room[] }` |
| GET | `/rooms/{roomId}` | all | `hours?` (default 6) | `RoomDetail` |
| GET | `/events` | staff, admin | `building?`, `room_id?`, `type?`, `since?`, `limit?`, `cursor?` | `Page<CampusEvent>` |
| GET | `/alerts` | staff, admin | `status?`, `severity?`, `type?`, `building?`, `limit?`, `cursor?` | `Page<Alert>` |
| PATCH | `/alerts/{alertId}` | staff, admin | body `AlertUpdate` | `Alert` |
| GET | `/service-requests` | all (students receive only their own) | `status?` (comma-separated), `priority?`, `escalated?`, `building?`, `limit?`, `cursor?` | `Page<ServiceRequest>` |
| POST | `/service-requests` | student | body `NewServiceRequest` | `201` `ServiceRequest` |
| GET | `/service-requests/{requestId}` | owner, staff, admin | — | `ServiceRequest` (with `history`) |
| PATCH | `/service-requests/{requestId}` | staff, admin | body `ServiceRequestUpdate` | `ServiceRequest` |
| GET | `/thresholds` | admin | — | `Thresholds` |
| PUT | `/thresholds` | admin | body `ThresholdsUpdate` | `Thresholds` |

`POST /events` and `POST /events/batch` also exist, but only sensors call them. The UI does not use them.

Error codes: `invalid_credentials` (401), `unauthorized` (401), `forbidden` (403), `not_found` (404), `validation_error` (400), `rate_limited` (429), `internal` (500).

## Types (`src/types/api.ts`)

```ts
export type Role = "student" | "staff" | "admin";
export type Severity = "normal" | "warning" | "critical";
export type RoomStatus = "empty" | "normal" | "busy" | "overloaded";
export type RoomType = "classroom" | "lecture_hall" | "lab" | "library" | "study_space";
export type EventType = "occupancy" | "temperature" | "humidity" | "energy" | "door" | "equipment";
export type AlertStatus = "open" | "acknowledged" | "resolved";
export type RequestStatus = "open" | "in_progress" | "resolved";
export type Priority = "low" | "medium" | "high" | "urgent";
export type RequestCategory =
  | "maintenance" | "it_support" | "cleaning" | "heating_cooling"
  | "access_badge" | "safety" | "medical" | "other";

export interface ApiError {
  error: { code: string; message: string };
}

export interface Page<T> {
  items: T[];
  next_cursor: string | null;
}

export interface Health {
  status: "ok";
  version: string;
  time: string;
}

export interface User {
  id: string;
  email: string;
  name: string;
  role: Role;
}

export interface LoginResponse {
  token: string;
  expires_at: string;
  user: User;
}

export interface Stats {
  generated_at: string;
  rooms: { total: number; occupied: number; busy: number; overloaded: number }; // occupied = occupancy > 0
  alerts: { open: number; critical: number; warning: number };
  service_requests: { open: number; urgent: number; escalated: number };
  energy_today: { total_kwh: number; top_building: { building: string; kwh: number } | null };
  ingestion: { events_last_hour: number; events_per_minute: number };
}

export interface BuildingEnergy {
  building: string;
  total_kwh: number;
  share: number;        // 0..1 of the day's campus total
  peak_hour: number;    // 0..23
  peak_kwh: number;
  hourly_kwh: number[]; // exactly 24 values, index = hour of day (campus time)
}

export interface EnergyStats {
  date: string;         // YYYY-MM-DD
  total_kwh: number;
  buildings: BuildingEnergy[]; // sorted by total_kwh, highest first
}

export interface Room {
  room_id: string;      // opaque ID, e.g. "library-a-a203"
  name: string;         // display name, e.g. "A203"
  building: string;
  floor: number;
  type: RoomType;
  capacity: number;
  occupancy: number;
  utilization: number;  // occupancy / capacity, e.g. 1.05 = 105%
  status: RoomStatus;
  temperature: number | null; // °C
  humidity: number | null;    // %
  open_alerts: number;
  last_updated: string;
}

export interface RoomReading {
  timestamp: string;
  occupancy: number | null;
  temperature: number | null;
  humidity: number | null;
}

export interface RoomDetail {
  room: Room;
  readings: RoomReading[]; // oldest first
  recent_events: CampusEvent[];
}

export interface CampusEvent {
  event_id: string;
  device_id: string;
  building: string;
  room_id: string | null; // null for building-level meters
  room: string | null;
  event_type: EventType;
  value: number | string; // door and equipment events use strings, e.g. "forced_open", "projector_failure"
  unit: string | null;    // "people", "celsius", "percent", "kWh", or null
  severity: Severity;
  timestamp: string;
}

export interface Alert {
  alert_id: string;
  event_id: string;
  building: string;
  room_id: string | null;
  room: string | null;
  type: EventType;
  severity: "warning" | "critical";
  message: string;        // e.g. "Temperature 38.2 °C is above the critical limit of 32 °C"
  value: number | string;
  unit: string | null;
  threshold: number | null;
  status: AlertStatus;
  created_at: string;
  updated_at: string;
  updated_by: string | null;
}

export interface AlertUpdate {
  status: "acknowledged" | "resolved";
  note?: string;
}

export interface RequestHistoryEntry {
  at: string;
  by: string; // display name, or "System" for automatic escalation
  action: "created" | "status_changed" | "assigned" | "escalated" | "de_escalated" | "note";
  detail: string | null; // e.g. "open → in_progress"
}

export interface ServiceRequest {
  request_id: string;
  title: string;
  description: string;
  category: RequestCategory;
  building: string;
  room_id: string | null;
  room: string | null;
  priority: Priority;               // set by the server
  status: RequestStatus;
  escalated: boolean;
  escalation_reason: string | null; // e.g. "Open for more than 60 minutes"
  created_by: { id: string; name: string };
  assigned_to: string | null;
  created_at: string;
  updated_at: string;
  due_at: string;
  history?: RequestHistoryEntry[];  // only in GET /service-requests/{requestId}
}

export interface NewServiceRequest {
  title: string;
  description: string;
  category: RequestCategory;
  building: string;
  room_id?: string;
}

export interface ServiceRequestUpdate {
  status?: RequestStatus;
  assigned_to?: string | null;
  escalated?: boolean;
  note?: string;
}

export interface ThresholdRange {
  warning_min: number;
  warning_max: number;
  critical_min: number;
  critical_max: number;
}

export interface Thresholds {
  occupancy: { busy_ratio: number; overloaded_ratio: number }; // e.g. 0.7 and 1.0
  temperature: ThresholdRange; // °C
  humidity: ThresholdRange;    // %
  energy: { warning_kwh_per_hour: number; critical_kwh_per_hour: number };
  service_requests: { urgent_categories: RequestCategory[]; escalate_after_minutes: number };
  updated_at: string;
  updated_by: string;
}

export type ThresholdsUpdate = Omit<Thresholds, "updated_at" | "updated_by">;
```
