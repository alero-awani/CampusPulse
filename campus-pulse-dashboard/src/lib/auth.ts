import { COGNITO_CLIENT_ID, COGNITO_DOMAIN } from "@/config";

// Sign-in with Cognito's hosted page, using the OAuth 2.0 authorization code flow
// with PKCE. The dashboard never sees the user's password: Cognito checks it and
// sends the browser back to /callback with a one-time code, which is exchanged
// for an ID token. The token is kept in sessionStorage, so it is gone when the tab closes.

export type Role = "student" | "staff" | "admin";

export type SessionUser = {
  name: string;
  email: string;
  role: Role | null;
};

export type Session = {
  idToken: string;
  expiresAt: number;
  user: SessionUser;
};

const SESSION_KEY = "campuspulse.session";
const PKCE_KEY = "campuspulse.pkce";

const redirectUri = () => `${window.location.origin}/callback`;

function base64url(bytes: Uint8Array): string {
  let binary = "";
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function randomString(byteLength: number): string {
  const bytes = new Uint8Array(byteLength);
  crypto.getRandomValues(bytes);
  return base64url(bytes);
}

/** Sends the browser to Cognito's sign-in page. */
export async function startSignIn(): Promise<void> {
  const verifier = randomString(48);
  const state = randomString(16);
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(verifier));
  sessionStorage.setItem(PKCE_KEY, JSON.stringify({ verifier, state }));

  const params = new URLSearchParams({
    response_type: "code",
    client_id: COGNITO_CLIENT_ID,
    redirect_uri: redirectUri(),
    scope: "openid email profile",
    state,
    code_challenge: base64url(new Uint8Array(digest)),
    code_challenge_method: "S256",
  });
  window.location.assign(`${COGNITO_DOMAIN}/oauth2/authorize?${params}`);
}

/** Finishes sign-in on /callback: checks the state and exchanges the code for tokens. */
export async function completeSignIn(query: URLSearchParams): Promise<Session> {
  const error = query.get("error");
  if (error) throw new Error(query.get("error_description") ?? error);

  const saved = JSON.parse(sessionStorage.getItem(PKCE_KEY) ?? "null") as
    | { verifier: string; state: string }
    | null;
  sessionStorage.removeItem(PKCE_KEY);
  const code = query.get("code");
  if (!code || !saved || saved.state !== query.get("state")) {
    throw new Error("Sign-in could not be verified. Please try again.");
  }

  const res = await fetch(`${COGNITO_DOMAIN}/oauth2/token`, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({
      grant_type: "authorization_code",
      client_id: COGNITO_CLIENT_ID,
      code,
      redirect_uri: redirectUri(),
      code_verifier: saved.verifier,
    }),
  });
  if (!res.ok) throw new Error(`Sign-in failed (${res.status}). Please try again.`);
  const data = (await res.json()) as { id_token: string; expires_in: number };

  const session: Session = {
    idToken: data.id_token,
    expiresAt: Date.now() + data.expires_in * 1000,
    user: userFromToken(data.id_token),
  };
  sessionStorage.setItem(SESSION_KEY, JSON.stringify(session));
  return session;
}

/** The current session, or null if signed out or expired. */
export function getSession(): Session | null {
  if (typeof window === "undefined") return null;
  const session = JSON.parse(sessionStorage.getItem(SESSION_KEY) ?? "null") as Session | null;
  if (!session || session.expiresAt <= Date.now()) {
    sessionStorage.removeItem(SESSION_KEY);
    return null;
  }
  return session;
}

/** Forgets the session and returns to the sign-in page without signing out of Cognito. */
export function endSession(): void {
  sessionStorage.removeItem(SESSION_KEY);
  window.location.assign("/login");
}

/** Signs out of the dashboard and of Cognito's hosted page. */
export function signOut(): void {
  sessionStorage.removeItem(SESSION_KEY);
  const params = new URLSearchParams({
    client_id: COGNITO_CLIENT_ID,
    logout_uri: `${window.location.origin}/login`,
  });
  window.location.assign(`${COGNITO_DOMAIN}/logout?${params}`);
}

function userFromToken(token: string): SessionUser {
  const payload = (token.split(".")[1] ?? "").replace(/-/g, "+").replace(/_/g, "/");
  const claims = JSON.parse(atob(payload)) as {
    email?: string;
    name?: string;
    "cognito:groups"?: string[];
  };
  const groups = claims["cognito:groups"] ?? [];
  const role = (["admin", "staff", "student"] as const).find((r) => groups.includes(r)) ?? null;
  return { name: claims.name ?? claims.email ?? "", email: claims.email ?? "", role };
}
