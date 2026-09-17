import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { AlertTriangle, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { completeSignIn } from "@/lib/auth";

export const Route = createFileRoute("/callback")({
  component: CallbackPage,
});

// Cognito sends the browser here after sign-in, with a one-time code.
function CallbackPage() {
  const navigate = useNavigate();
  const [error, setError] = useState<string | null>(null);
  const started = useRef(false);

  useEffect(() => {
    // The code can only be used once, so don't run twice (React dev mode mounts twice).
    if (started.current) return;
    started.current = true;
    completeSignIn(new URLSearchParams(window.location.search))
      .then(() => navigate({ to: "/", replace: true }))
      .catch((err: unknown) => setError(err instanceof Error ? err.message : "Sign-in failed"));
  }, [navigate]);

  return (
    <main className="flex min-h-screen items-center justify-center bg-background px-6">
      {error ? (
        <Card className="w-full max-w-md gap-0 rounded-2xl border-danger/40 bg-danger/5 p-6">
          <div className="flex items-center gap-3">
            <AlertTriangle className="size-5 text-danger" />
            <p className="font-medium text-foreground">Couldn't sign you in</p>
          </div>
          <p className="mt-2 text-sm text-muted-foreground">{error}</p>
          <Button className="mt-5" onClick={() => void navigate({ to: "/login", replace: true })}>
            Back to sign in
          </Button>
        </Card>
      ) : (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <RefreshCw className="size-4 animate-spin" />
          Signing you in…
        </p>
      )}
    </main>
  );
}
