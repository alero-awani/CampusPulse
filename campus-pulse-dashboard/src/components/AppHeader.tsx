import { Link } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { LogOut, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { endSession, getSession, signOut, type SessionUser } from "@/lib/auth";

const linkBase =
  "rounded-full px-3 py-1.5 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground";
const linkActive = "bg-secondary text-foreground";

export function AppHeader({
  ago,
  loading,
  onRefresh,
}: {
  ago: string;
  loading: boolean;
  onRefresh: () => void;
}) {
  // Read the session in the browser only, so the server-rendered page matches.
  const [user, setUser] = useState<SessionUser | null>(null);
  useEffect(() => {
    const session = getSession();
    if (session) setUser(session.user);
    else endSession();
  }, []);

  return (
    <header className="flex flex-wrap items-end justify-between gap-4 border-b border-border/70 pb-6">
      <div>
        <h1 className="font-display text-3xl font-bold text-foreground">CampusPulse</h1>
        <p className="mt-1 text-sm text-muted-foreground">NorthBridge University</p>
        <nav className="mt-3 flex items-center gap-1">
          <Link to="/" className={linkBase} activeOptions={{ exact: true }} activeProps={{ className: linkActive }}>
            Overview
          </Link>
          <Link to="/operations" className={linkBase} activeProps={{ className: linkActive }}>
            Operations
          </Link>
          <Link to="/energy" className={linkBase} activeProps={{ className: linkActive }}>
            Energy
          </Link>
        </nav>
      </div>
      <div className="flex items-center gap-4">
        <span className="text-sm text-muted-foreground">Updated {ago}</span>
        <Button onClick={onRefresh} disabled={loading} variant="secondary">
          <RefreshCw className={loading ? "animate-spin" : undefined} />
          Refresh
        </Button>
        {user && (
          <div className="flex items-center gap-3 border-l border-border/70 pl-4">
            <div className="text-right">
              <p className="text-sm font-medium text-foreground">{user.name}</p>
              <p className="text-xs capitalize text-muted-foreground">{user.role ?? "No role"}</p>
            </div>
            <Button onClick={signOut} variant="ghost" size="icon" aria-label="Sign out" title="Sign out">
              <LogOut />
            </Button>
          </div>
        )}
      </div>
    </header>
  );
}
