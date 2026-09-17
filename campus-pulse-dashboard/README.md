# Campus Pulse Dashboard

Build a single-page dashboard called "CampusPulse" for NorthBridge University.
Frontend only: React, TypeScript, Tailwind, shadcn/ui. Do NOT add Supabase,
Lovable Cloud, a login page, routing, or any other pages.

Put these constants in src/config.ts:
API_BASE_URL = "http://localhost:8080"
DEMO_EMAIL = "staff@northbridge.edu"
DEMO_PASSWORD = "demo1234"

On page load, sign in silently: POST {API_BASE_URL}/auth/login with JSON
{"email": DEMO_EMAIL, "password": DEMO_PASSWORD}. The response contains "token".
Send "Authorization: Bearer <token>" on all other requests. Refresh the data
every 30 seconds.

Layout:
1. Header: "CampusPulse", "NorthBridge University", "Updated X ago", Refresh button.
2. Six stat cards from GET /stats:
   - Rooms occupied: rooms.occupied / rooms.total (small text: rooms.busy busy)
   - Overloaded rooms: rooms.overloaded (red when > 0)
   - Open alerts: alerts.open (small text: alerts.critical critical, alerts.warning warning; red when critical > 0)
   - Urgent requests: service_requests.urgent (small text: service_requests.escalated escalated)
   - Energy today: energy_today.total_kwh kWh (small text: top_building.building, or "No data")
   - Events last hour: ingestion.events_last_hour (small text: ingestion.events_per_minute per minute)
3. Rooms table from GET /rooms: building, room name, occupancy/capacity with a
   progress bar, status badge (overloaded red, busy amber, normal green, empty gray),
   temperature °C, humidity %. Sort: overloaded, busy, normal, empty.

Show skeletons while loading, and an error message with a Retry button if a request fails.

Response shapes:
GET /stats -> { rooms: {total, occupied, busy, overloaded},
  alerts: {open, critical, warning},
  service_requests: {open, urgent, escalated},
  energy_today: {total_kwh, top_building: {building, kwh} | null},
  ingestion: {events_last_hour, events_per_minute} }
GET /rooms -> { items: [{ room_id, name, building, type, capacity, occupancy,
  utilization, status: "empty"|"normal"|"busy"|"overloaded",
  temperature: number|null, humidity: number|null }] }

This project was built with [Lovable](https://lovable.dev).

## Build with Lovable

Continue developing this project in the [Lovable editor](https://lovable.dev/projects/c563030a-af3a-44cc-950f-6f2023669951).

- **Ship faster**: describe what you want to build and Lovable handles the code.
- **Stay in sync**: every change made in Lovable is committed straight to this repository.
- **Full ownership**: this code is yours. Push to `main` on GitHub and your changes sync back into Lovable, ready for your next prompt.

## Development

Prefer working locally? You need Node.js and npm — [install with nvm](https://github.com/nvm-sh/nvm#installing-and-updating).

```sh
git clone <this-repository-url>
cd <repository-name>
npm i
npm run dev
```
