import { createFileRoute } from "@tanstack/react-router";
import { useCallback, useEffect, useRef, useState } from "react";
import { AlertTriangle } from "lucide-react";
import {
  Bar,
  BarChart,
  CartesianGrid,
  LabelList,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { AppHeader } from "@/components/AppHeader";
import { useAgo } from "@/lib/useAgo";
import { fetchEnergy, type EnergyStats } from "@/lib/api";

export const Route = createFileRoute("/energy")({
  head: () => ({
    meta: [
      { title: "Energy — CampusPulse | NorthBridge University" },
      {
        name: "description",
        content: "Which NorthBridge University buildings use the most energy, hour by hour.",
      },
    ],
  }),
  component: EnergyPage,
});

const COLORS = ["var(--chart-1)", "var(--chart-2)", "var(--chart-3)", "var(--chart-4)", "var(--chart-5)"];

function today(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

const hourLabel = (h: number) => `${String(h).padStart(2, "0")}:00`;

function EnergyPage() {
  const [date, setDate] = useState(today);
  const [energy, setEnergy] = useState<EnergyStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [updatedAt, setUpdatedAt] = useState<Date | null>(null);
  const inFlight = useRef(false);

  const load = useCallback(async () => {
    if (inFlight.current) return;
    inFlight.current = true;
    setLoading(true);
    try {
      setEnergy(await fetchEnergy(date));
      setUpdatedAt(new Date());
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Something went wrong");
    } finally {
      setLoading(false);
      inFlight.current = false;
    }
  }, [date]);

  useEffect(() => {
    void load();
    const id = setInterval(() => void load(), 30_000);
    return () => clearInterval(id);
  }, [load]);

  const ago = useAgo(updatedAt);
  const buildings = energy?.buildings ?? [];
  const hourly = Array.from({ length: 24 }, (_, h) => {
    const row: Record<string, number | string> = { hour: hourLabel(h) };
    for (const b of buildings) row[b.building] = b.hourly_kwh[h] ?? 0;
    return row;
  });

  return (
    <main className="min-h-screen bg-background">
      <div className="mx-auto max-w-7xl px-6 py-8">
        <AppHeader ago={ago} loading={loading} onRefresh={() => void load()} />

        {error && (
          <Card className="mt-6 flex flex-row items-center justify-between gap-4 rounded-2xl border-danger/40 bg-danger/5 p-5">
            <div className="flex items-center gap-3">
              <AlertTriangle className="size-5 text-danger" />
              <div>
                <p className="font-medium text-foreground">Couldn't load energy data</p>
                <p className="text-sm text-muted-foreground">{error}</p>
              </div>
            </div>
            <Button onClick={() => void load()} disabled={loading}>
              Retry
            </Button>
          </Card>
        )}

        <section className="mt-6 flex flex-wrap items-end justify-between gap-4">
          <div>
            <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
              Campus total
            </p>
            {energy ? (
              <p className="mt-1 font-display text-3xl font-semibold text-foreground">
                {energy.total_kwh.toLocaleString()} kWh
              </p>
            ) : (
              <Skeleton className="mt-2 h-8 w-40" />
            )}
          </div>
          <label className="flex items-center gap-2 text-sm text-muted-foreground">
            Date
            <Input
              type="date"
              value={date}
              max={today()}
              onChange={(e) => e.target.value && setDate(e.target.value)}
              className="w-44"
            />
          </label>
        </section>

        <section className="mt-6 grid gap-4 lg:grid-cols-2">
          <Card className="gap-0 rounded-2xl border-border/70 p-5 shadow-sm">
            <h2 className="font-display text-lg font-semibold text-foreground">Usage by building</h2>
            <p className="text-sm text-muted-foreground">Highest first</p>
            <div className="mt-4 h-72">
              {!energy ? (
                <Skeleton className="h-full w-full" />
              ) : (
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={buildings} layout="vertical" margin={{ left: 10, right: 60 }}>
                    <CartesianGrid horizontal={false} strokeDasharray="3 3" />
                    <XAxis type="number" unit=" kWh" tick={{ fontSize: 12 }} />
                    <YAxis type="category" dataKey="building" width={110} tick={{ fontSize: 12 }} />
                    <Tooltip formatter={(v: number) => [`${v} kWh`, "Total"]} />
                    <Bar dataKey="total_kwh" fill="var(--chart-1)" radius={[0, 6, 6, 0]}>
                      <LabelList
                        dataKey="share"
                        position="right"
                        formatter={(v: number) => `${Math.round(v * 100)}%`}
                        style={{ fontSize: 12 }}
                      />
                    </Bar>
                  </BarChart>
                </ResponsiveContainer>
              )}
            </div>
          </Card>

          <Card className="gap-0 rounded-2xl border-border/70 p-5 shadow-sm">
            <h2 className="font-display text-lg font-semibold text-foreground">Hourly usage</h2>
            <p className="text-sm text-muted-foreground">kWh per hour, campus time</p>
            <div className="mt-4 h-72">
              {!energy ? (
                <Skeleton className="h-full w-full" />
              ) : (
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={hourly} margin={{ right: 10 }}>
                    <CartesianGrid strokeDasharray="3 3" />
                    <XAxis dataKey="hour" interval={3} tick={{ fontSize: 12 }} />
                    <YAxis tick={{ fontSize: 12 }} />
                    <Tooltip formatter={(v: number) => `${v} kWh`} />
                    <Legend wrapperStyle={{ fontSize: 12 }} />
                    {buildings.map((b, i) => (
                      <Line
                        key={b.building}
                        type="monotone"
                        dataKey={b.building}
                        stroke={COLORS[i % COLORS.length]}
                        strokeWidth={2}
                        dot={false}
                      />
                    ))}
                  </LineChart>
                </ResponsiveContainer>
              )}
            </div>
          </Card>
        </section>

        <section className="mt-8">
          <h2 className="mb-3 font-display text-lg font-semibold text-foreground">Buildings</h2>
          <Card className="overflow-hidden rounded-2xl border-border/70 p-0 shadow-sm">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Building</TableHead>
                  <TableHead className="text-right">Total</TableHead>
                  <TableHead className="text-right">Share</TableHead>
                  <TableHead className="text-right">Peak hour</TableHead>
                  <TableHead className="text-right">Peak usage</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {!energy
                  ? Array.from({ length: 4 }).map((_, i) => (
                      <TableRow key={i}>
                        {Array.from({ length: 5 }).map((__, j) => (
                          <TableCell key={j}>
                            <Skeleton className="h-4 w-full" />
                          </TableCell>
                        ))}
                      </TableRow>
                    ))
                  : buildings.map((b) => (
                      <TableRow key={b.building}>
                        <TableCell className="font-medium text-foreground">{b.building}</TableCell>
                        <TableCell className="text-right tabular-nums">{b.total_kwh} kWh</TableCell>
                        <TableCell className="text-right tabular-nums">{Math.round(b.share * 100)}%</TableCell>
                        <TableCell className="text-right tabular-nums">
                          {b.total_kwh > 0 ? `${hourLabel(b.peak_hour)}–${hourLabel((b.peak_hour + 1) % 24)}` : "—"}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {b.total_kwh > 0 ? `${b.peak_kwh} kWh` : "—"}
                        </TableCell>
                      </TableRow>
                    ))}
              </TableBody>
            </Table>
          </Card>
        </section>
      </div>
    </main>
  );
}
