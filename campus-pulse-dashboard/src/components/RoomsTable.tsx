import { cn } from "@/lib/utils";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { STATUS_ORDER, type Room, type RoomStatus } from "@/lib/api";
import { formatType } from "@/lib/useAgo";

const statusStyles: Record<RoomStatus, string> = {
  overloaded: "bg-danger/15 text-danger",
  busy: "bg-warning/20 text-warning-foreground",
  normal: "bg-success/20 text-success-foreground",
  empty: "bg-neutral/40 text-neutral-foreground",
};

const barStyles: Record<RoomStatus, string> = {
  overloaded: "bg-danger",
  busy: "bg-warning",
  normal: "bg-success",
  empty: "bg-neutral",
};

function StatusBadge({ status }: { status: RoomStatus }) {
  return (
    <span
      className={cn(
        "inline-flex rounded-full px-2.5 py-0.5 text-xs font-semibold capitalize",
        statusStyles[status],
      )}
    >
      {status}
    </span>
  );
}

export function RoomsTable({ rooms, loading }: { rooms: Room[]; loading?: boolean }) {
  const sorted = [...rooms].sort(
    (a, b) =>
      STATUS_ORDER[a.status] - STATUS_ORDER[b.status] ||
      a.building.localeCompare(b.building) ||
      a.name.localeCompare(b.name),
  );

  return (
    <Card className="overflow-hidden rounded-2xl border-border/70 p-0 shadow-sm">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead>Building</TableHead>
            <TableHead>Room</TableHead>
            <TableHead>Type</TableHead>
            <TableHead className="w-[240px]">Occupancy</TableHead>
            <TableHead>Status</TableHead>
            <TableHead className="text-right">Temp</TableHead>
            <TableHead className="text-right">Humidity</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading
            ? Array.from({ length: 6 }).map((_, i) => (
                <TableRow key={i}>
                  {Array.from({ length: 7 }).map((__, j) => (
                    <TableCell key={j}>
                      <Skeleton className="h-4 w-full" />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            : sorted.map((room) => {
                const pct = room.capacity
                  ? Math.min(100, Math.round((room.occupancy / room.capacity) * 100))
                  : 0;
                return (
                  <TableRow key={room.room_id}>
                    <TableCell className="text-muted-foreground">{room.building}</TableCell>
                    <TableCell className="font-medium text-foreground">{room.name}</TableCell>
                    <TableCell className="text-muted-foreground">{formatType(room.type)}</TableCell>
                    <TableCell>
                      <div className="flex items-center gap-3">
                        <span className="w-14 shrink-0 tabular-nums text-sm">
                          {room.occupancy}/{room.capacity}
                        </span>
                        <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
                          <div
                            className={cn("h-full rounded-full", barStyles[room.status])}
                            style={{ width: `${pct}%` }}
                          />
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={room.status} />
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {room.temperature === null ? "—" : `${room.temperature.toFixed(1)} °C`}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {room.humidity === null ? "—" : `${Math.round(room.humidity)}%`}
                    </TableCell>
                  </TableRow>
                );
              })}
          {!loading && sorted.length === 0 && (
            <TableRow>
              <TableCell colSpan={7} className="py-10 text-center text-muted-foreground">
                No rooms reporting right now.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </Card>
  );
}
