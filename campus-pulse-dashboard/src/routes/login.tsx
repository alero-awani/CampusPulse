import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { BellRing, DoorOpen, LogIn, ShieldCheck, Zap } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { getSession, startSignIn } from "@/lib/auth";

export const Route = createFileRoute("/login")({
  head: () => ({
    meta: [
      { title: "Sign in — CampusPulse | NorthBridge University" },
      { name: "description", content: "Sign in to the CampusPulse operations dashboard." },
    ],
  }),
  component: LoginPage,
});

const features = [
  { icon: DoorOpen, label: "Live room occupancy" },
  { icon: BellRing, label: "Alerts and service requests" },
  { icon: Zap, label: "Energy use by building" },
];

function LoginPage() {
  const navigate = useNavigate();
  const [redirecting, setRedirecting] = useState(false);

  useEffect(() => {
    if (getSession()) void navigate({ to: "/", replace: true });
  }, [navigate]);

  const signIn = () => {
    setRedirecting(true);
    void startSignIn();
  };

  return (
    <main className="flex min-h-screen items-center justify-center bg-background px-6 py-12">
      <Card className="w-full max-w-md gap-0 rounded-2xl border-border/70 p-8 shadow-sm">
        <h1 className="font-display text-3xl font-bold text-foreground">CampusPulse</h1>
        <p className="mt-1 text-sm text-muted-foreground">NorthBridge University</p>

        <p className="mt-6 text-foreground">
          The operations dashboard for campus staff: see what is happening across
          campus and act on it.
        </p>

        <ul className="mt-6 space-y-3">
          {features.map(({ icon: Icon, label }) => (
            <li key={label} className="flex items-center gap-3 text-sm text-foreground">
              <span className="flex size-8 items-center justify-center rounded-full bg-secondary text-muted-foreground">
                <Icon className="size-4" />
              </span>
              {label}
            </li>
          ))}
        </ul>

        <Button onClick={signIn} disabled={redirecting} size="lg" className="mt-8 w-full">
          <LogIn />
          {redirecting ? "Opening sign-in…" : "Sign in"}
        </Button>

        <p className="mt-4 flex items-center justify-center gap-1.5 text-xs text-muted-foreground">
          <ShieldCheck className="size-3.5" />
          Secured by Amazon Cognito
        </p>
      </Card>
    </main>
  );
}
