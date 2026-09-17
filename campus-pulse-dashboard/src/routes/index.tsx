import { createFileRoute } from "@tanstack/react-router";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  AlertTriangle,
  Activity,
  BellRing,
  DoorOpen,
  Wrench,
  Zap,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { AppHeader } from "@/components/AppHeader";
import { StatCard } from "@/components/StatCard";
import { RoomsTable } from "@/components/RoomsTable";
import { fetchRooms, fetchStats, type Room, type Stats } from "@/lib/api";

export const Route = createFileRoute("/")({
  head: () => ({
    meta: [
      { title: "CampusPulse — NorthBridge University Live Dashboard" },
      {
        name: "description",
        content:
          "Live campus operations for NorthBridge University: room occupancy, alerts, service requests and energy use, refreshed every 30 seconds.",
      },
      { property: "og:title", content: "CampusPulse — NorthBridge University" },
      {
        property: "og:description",
        content:
          "Real-time room occupancy, alerts, service requests and energy use across NorthBridge University.",
      },
      { property: "og:type", content: "website" },
      { name: "twitter:card", content: "summary_large_image" },
    ],
  }),
  component: Dashboard,
});

function useAgo(date: Date | null) {
  const [, tick] = useState(0);
  useEffect(() => {
    const id = setInterval(() => tick((t) => t + 1), 1000);
    return () => clearInterval(id);
  }, []);
  if (!date) return "never";
  const s = Math.max(0, Math.round((Date.now() - date.getTime()) / 1000));
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  return `${Math.floor(m / 60)}h ago`;
}

function Dashboard() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [rooms, setRooms] = useState<Room[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [updatedAt, setUpdatedAt] = useState<Date | null>(null);
  const inFlight = useRef(false);

  const load = useCallback(async () => {
    if (inFlight.current) return;
    inFlight.current = true;
    setLoading(true);
    try {
      const [s, r] = await Promise.all([fetchStats(), fetchRooms()]);
      setStats(s);
      setRooms(r.items ?? []);
      setUpdatedAt(new Date());
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Something went wrong");
    } finally {
      setLoading(false);
      inFlight.current = false;
    }
  }, []);

  useEffect(() => {
    void load();
    const id = setInterval(() => void load(), 30_000);
    return () => clearInterval(id);
  }, [load]);

  const ago = useAgo(updatedAt);
  const firstLoad = loading && !stats && !rooms;

  return (
    <main className="min-h-screen bg-background">
      <div className="mx-auto max-w-7xl px-6 py-8">
        <AppHeader ago={ago} loading={loading} onRefresh={() => void load()} />

        {error && (
          <Card className="mt-6 flex flex-row items-center justify-between gap-4 rounded-2xl border-danger/40 bg-danger/5 p-5">
            <div className="flex items-center gap-3">
              <AlertTriangle className="size-5 text-danger" />
              <div>
                <p className="font-medium text-foreground">Couldn't load campus data</p>
                <p className="text-sm text-muted-foreground">{error}</p>
              </div>
            </div>
            <Button onClick={() => void load()} disabled={loading}>
              Retry
            </Button>
          </Card>
        )}

        <section className="mt-6 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {firstLoad || !stats
            ? Array.from({ length: 6 }).map((_, i) => (
                <Card key={i} className="gap-0 rounded-2xl p-5">
                  <Skeleton className="h-3 w-24" />
                  <Skeleton className="mt-4 h-8 w-20" />
                  <Skeleton className="mt-3 h-3 w-28" />
                </Card>
              ))
            : (() => {
                const s = stats;
                return (
                  <>
                    <StatCard
                      label="Rooms occupied"
                      value={`${s.rooms.occupied} / ${s.rooms.total}`}
                      hint={`${s.rooms.busy} busy`}
                      icon={<DoorOpen className="size-5" />}
                    />
                    <StatCard
                      label="Overloaded rooms"
                      value={s.rooms.overloaded}
                      alert={s.rooms.overloaded > 0}
                      icon={<AlertTriangle className="size-5" />}
                    />
                    <StatCard
                      label="Open alerts"
                      value={s.alerts.open}
                      hint={`${s.alerts.critical} critical, ${s.alerts.warning} warning`}
                      alert={s.alerts.critical > 0}
                      icon={<BellRing className="size-5" />}
                    />
                    <StatCard
                      label="Urgent requests"
                      value={s.service_requests.urgent}
                      hint={`${s.service_requests.escalated} escalated`}
                      icon={<Wrench className="size-5" />}
                    />
                    <StatCard
                      label="Energy today"
                      value={`${s.energy_today.total_kwh} kWh`}
                      hint={
                        s.energy_today.top_building
                          ? `Top: ${s.energy_today.top_building.building} (${s.energy_today.top_building.kwh} kWh)`
                          : "No data yet"
                      }
                      icon={<Zap className="size-5" />}
                    />
                    <StatCard
                      label="Events last hour"
                      value={s.ingestion.events_last_hour}
                      hint={`${s.ingestion.events_per_minute} per minute`}
                      icon={<Activity className="size-5" />}
                    />
                  </>
                );
              })()}
        </section>

        <section className="mt-8">
          <h2 className="mb-3 font-display text-lg font-semibold text-foreground">Rooms</h2>
          <RoomsTable rooms={rooms ?? []} loading={firstLoad || !rooms} />
        </section>
      </div>
    </main>
  );
}
