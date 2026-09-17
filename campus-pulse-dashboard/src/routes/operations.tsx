import { createFileRoute } from "@tanstack/react-router";
import { useCallback, useEffect, useRef, useState } from "react";
import { AlertTriangle } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { AppHeader } from "@/components/AppHeader";
import { cn } from "@/lib/utils";
import { useAgo, formatAgo, formatType } from "@/lib/useAgo";
import {
  fetchAlerts,
  fetchServiceRequests,
  updateAlert,
  updateServiceRequest,
  PRIORITY_ORDER,
  type Alert,
  type AlertStatus,
  type RequestPriority,
  type RequestStatus,
  type ServiceRequest,
} from "@/lib/api";

export const Route = createFileRoute("/operations")({
  head: () => ({
    meta: [
      { title: "Operations — CampusPulse | NorthBridge University" },
      {
        name: "description",
        content:
          "Triage campus alerts and service requests for NorthBridge University: acknowledge, start and resolve work from one live queue.",
      },
      { property: "og:title", content: "Operations — CampusPulse" },
      {
        property: "og:description",
        content:
          "Live alert and service-request queues for NorthBridge University facilities teams.",
      },
      { property: "og:type", content: "website" },
      { name: "twitter:card", content: "summary_large_image" },
    ],
  }),
  component: OperationsPage,
});

const severityStyles: Record<Alert["severity"], string> = {
  critical: "bg-danger/15 text-danger",
  warning: "bg-warning/20 text-warning-foreground",
};

const priorityStyles: Record<RequestPriority, string> = {
  urgent: "bg-danger/15 text-danger",
  high: "bg-orange-500/15 text-orange-600",
  medium: "bg-blue-500/15 text-blue-600",
  low: "bg-neutral/40 text-neutral-foreground",
};

function Pill({ className, children }: { className?: string; children: React.ReactNode }) {
  return (
    <span
      className={cn(
        "inline-flex rounded-full px-2.5 py-0.5 text-xs font-semibold capitalize",
        className,
      )}
    >
      {children}
    </span>
  );
}

function Loc({ building, room }: { building: string; room: string | null }) {
  return (
    <span className="text-muted-foreground">
      {building}
      {room ? ` / ${room}` : ""}
    </span>
  );
}

function LoadingRows({ cols }: { cols: number }) {
  return (
    <>
      {Array.from({ length: 4 }).map((_, i) => (
        <TableRow key={i}>
          {Array.from({ length: cols }).map((__, j) => (
            <TableCell key={j}>
              <Skeleton className="h-4 w-full" />
            </TableCell>
          ))}
        </TableRow>
      ))}
    </>
  );
}

function OperationsPage() {
  const [alertStatus, setAlertStatus] = useState<AlertStatus>("open");
  const [requestTab, setRequestTab] = useState<"active" | "resolved">("active");
  const [alerts, setAlerts] = useState<Alert[] | null>(null);
  const [requests, setRequests] = useState<ServiceRequest[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [updatedAt, setUpdatedAt] = useState<Date | null>(null);
  const [pending, setPending] = useState<string | null>(null);
  const inFlight = useRef(false);

  const load = useCallback(async () => {
    if (inFlight.current) return;
    inFlight.current = true;
    setLoading(true);
    try {
      const statuses: RequestStatus[] =
        requestTab === "active" ? ["open", "in_progress"] : ["resolved"];
      const [a, r] = await Promise.all([fetchAlerts(alertStatus), fetchServiceRequests(statuses)]);
      setAlerts(a.items ?? []);
      setRequests(r.items ?? []);
      setUpdatedAt(new Date());
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Something went wrong");
    } finally {
      setLoading(false);
      inFlight.current = false;
    }
  }, [alertStatus, requestTab]);

  useEffect(() => {
    void load();
    const id = setInterval(() => void load(), 30_000);
    return () => clearInterval(id);
  }, [load]);

  const ago = useAgo(updatedAt);

  const act = async (key: string, fn: () => Promise<void>) => {
    setPending(key);
    try {
      await fn();
      await load();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Action failed");
    } finally {
      setPending(null);
    }
  };

  const sortedAlerts = [...(alerts ?? [])].sort(
    (a, b) =>
      (a.severity === "critical" ? 0 : 1) - (b.severity === "critical" ? 0 : 1) ||
      new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
  );

  const sortedRequests = [...(requests ?? [])].sort(
    (a, b) =>
      Number(b.escalated) - Number(a.escalated) ||
      PRIORITY_ORDER[a.priority] - PRIORITY_ORDER[b.priority] ||
      new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
  );

  return (
    <main className="min-h-screen bg-background">
      <div className="mx-auto max-w-7xl px-6 py-8">
        <AppHeader ago={ago} loading={loading} onRefresh={() => void load()} />

        {error && (
          <Card className="mt-6 flex flex-row items-center justify-between gap-4 rounded-2xl border-danger/40 bg-danger/5 p-5">
            <div className="flex items-center gap-3">
              <AlertTriangle className="size-5 text-danger" />
              <div>
                <p className="font-medium text-foreground">Couldn't load operations data</p>
                <p className="text-sm text-muted-foreground">{error}</p>
              </div>
            </div>
            <Button onClick={() => void load()} disabled={loading}>
              Retry
            </Button>
          </Card>
        )}

        <section className="mt-8">
          <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
            <h2 className="font-display text-lg font-semibold text-foreground">Alerts</h2>
            <Tabs value={alertStatus} onValueChange={(v) => setAlertStatus(v as AlertStatus)}>
              <TabsList>
                <TabsTrigger value="open">Open</TabsTrigger>
                <TabsTrigger value="acknowledged">Acknowledged</TabsTrigger>
                <TabsTrigger value="resolved">Resolved</TabsTrigger>
              </TabsList>
            </Tabs>
          </div>
          <Card className="overflow-hidden rounded-2xl border-border/70 p-0 shadow-sm">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Severity</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Location</TableHead>
                  <TableHead>Message</TableHead>
                  <TableHead>Raised</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {alerts === null ? (
                  <LoadingRows cols={6} />
                ) : sortedAlerts.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={6} className="py-10 text-center text-muted-foreground">
                      No {alertStatus} alerts.
                    </TableCell>
                  </TableRow>
                ) : (
                  sortedAlerts.map((a) => (
                    <TableRow key={a.alert_id}>
                      <TableCell>
                        <Pill className={severityStyles[a.severity]}>{a.severity}</Pill>
                      </TableCell>
                      <TableCell className="font-medium text-foreground">
                        {formatType(a.type)}
                      </TableCell>
                      <TableCell>
                        <Loc building={a.building} room={a.room} />
                      </TableCell>
                      <TableCell className="max-w-[420px] text-muted-foreground">
                        {a.message}
                      </TableCell>
                      <TableCell className="whitespace-nowrap text-muted-foreground">
                        {formatAgo(a.created_at)}
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end gap-2">
                          {a.status === "open" && (
                            <Button
                              size="sm"
                              variant="secondary"
                              disabled={pending === a.alert_id}
                              onClick={() =>
                                void act(a.alert_id, () => updateAlert(a.alert_id, "acknowledged"))
                              }
                            >
                              Acknowledge
                            </Button>
                          )}
                          {a.status !== "resolved" && (
                            <Button
                              size="sm"
                              disabled={pending === a.alert_id}
                              onClick={() =>
                                void act(a.alert_id, () => updateAlert(a.alert_id, "resolved"))
                              }
                            >
                              Resolve
                            </Button>
                          )}
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </Card>
        </section>

        <section className="mt-10">
          <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
            <h2 className="font-display text-lg font-semibold text-foreground">
              Service requests
            </h2>
            <Tabs
              value={requestTab}
              onValueChange={(v) => setRequestTab(v as "active" | "resolved")}
            >
              <TabsList>
                <TabsTrigger value="active">Active</TabsTrigger>
                <TabsTrigger value="resolved">Resolved</TabsTrigger>
              </TabsList>
            </Tabs>
          </div>
          <Card className="overflow-hidden rounded-2xl border-border/70 p-0 shadow-sm">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Priority</TableHead>
                  <TableHead>Request</TableHead>
                  <TableHead>Category</TableHead>
                  <TableHead>Location</TableHead>
                  <TableHead>Requested by</TableHead>
                  <TableHead>Age</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {requests === null ? (
                  <LoadingRows cols={8} />
                ) : sortedRequests.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8} className="py-10 text-center text-muted-foreground">
                      No {requestTab} service requests.
                    </TableCell>
                  </TableRow>
                ) : (
                  sortedRequests.map((r) => {
                    const overdue =
                      r.status !== "resolved" &&
                      r.due_at !== null &&
                      new Date(r.due_at).getTime() < Date.now();
                    return (
                      <TableRow key={r.request_id}>
                        <TableCell>
                          <div className="flex flex-wrap items-center gap-1.5">
                            <Pill className={priorityStyles[r.priority]}>{r.priority}</Pill>
                            {r.escalated && (
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <span>
                                    <Pill className="cursor-help bg-danger/15 text-danger">
                                      Escalated
                                    </Pill>
                                  </span>
                                </TooltipTrigger>
                                <TooltipContent>
                                  {r.escalation_reason ?? "Escalated"}
                                </TooltipContent>
                              </Tooltip>
                            )}
                          </div>
                        </TableCell>
                        <TableCell className="font-medium text-foreground">
                          <div className="flex flex-wrap items-center gap-1.5">
                            {r.title}
                            {overdue && (
                              <Pill className="bg-danger/15 text-danger">Overdue</Pill>
                            )}
                          </div>
                        </TableCell>
                        <TableCell className="text-muted-foreground">{r.category}</TableCell>
                        <TableCell>
                          <Loc building={r.building} room={r.room} />
                        </TableCell>
                        <TableCell className="text-muted-foreground">
                          {r.created_by?.name ?? "—"}
                        </TableCell>
                        <TableCell className="whitespace-nowrap text-muted-foreground">
                          {formatAgo(r.created_at)}
                        </TableCell>
                        <TableCell>
                          <Pill className="bg-neutral/40 text-neutral-foreground">
                            {formatType(r.status)}
                          </Pill>
                        </TableCell>
                        <TableCell className="text-right">
                          <div className="flex justify-end gap-2">
                            {r.status === "open" && (
                              <Button
                                size="sm"
                                variant="secondary"
                                disabled={pending === r.request_id}
                                onClick={() =>
                                  void act(r.request_id, () =>
                                    updateServiceRequest(r.request_id, "in_progress"),
                                  )
                                }
                              >
                                Start
                              </Button>
                            )}
                            {r.status !== "resolved" && (
                              <Button
                                size="sm"
                                disabled={pending === r.request_id}
                                onClick={() =>
                                  void act(r.request_id, () =>
                                    updateServiceRequest(r.request_id, "resolved"),
                                  )
                                }
                              >
                                Resolve
                              </Button>
                            )}
                          </div>
                        </TableCell>
                      </TableRow>
                    );
                  })
                )}
              </TableBody>
            </Table>
          </Card>
        </section>
      </div>
    </main>
  );
}
