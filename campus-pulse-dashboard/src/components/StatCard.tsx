import type { ReactNode } from "react";
import { cn } from "@/lib/utils";
import { Card } from "@/components/ui/card";

type Props = {
  label: string;
  value: ReactNode;
  hint?: string;
  alert?: boolean;
  icon: ReactNode;
};

export function StatCard({ label, value, hint, alert, icon }: Props) {
  return (
    <Card
      className={cn(
        "gap-0 rounded-2xl border-border/70 p-5 shadow-sm transition-colors",
        alert && "border-danger/40 bg-danger/5",
      )}
    >
      <div className="flex items-start justify-between">
        <span className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {label}
        </span>
        <span className={cn("text-muted-foreground", alert && "text-danger")}>{icon}</span>
      </div>
      <div
        className={cn(
          "mt-3 font-display text-3xl font-semibold text-foreground",
          alert && "text-danger",
        )}
      >
        {value}
      </div>
      <p className="mt-1 text-xs text-muted-foreground">{hint ?? "\u00a0"}</p>
    </Card>
  );
}
