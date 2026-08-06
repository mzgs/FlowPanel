import { PasswordInput } from "@/components/password-input";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { FlowPanelMark } from "@/components/flowpanel-mark";

type AuthPageProps = {
  setupRequired: boolean;
};

export function AuthPage({ setupRequired }: AuthPageProps) {
  const error = new URLSearchParams(window.location.search).get("auth_error");

  return (
    <main className="flex min-h-screen items-center justify-center bg-[var(--app-bg)] px-4 py-6">
      <form
        action="/api/auth/login"
        method="post"
        autoComplete="on"
        className="w-full max-w-[360px] rounded-xl border border-[var(--app-border)] bg-[var(--app-bg-2)] p-5 shadow-[var(--app-shadow)]"
      >
        <div className="mb-5 flex items-start gap-3">
          <FlowPanelMark className="h-9 w-9 shrink-0" />
          <div className="min-w-0">
            <h1 className="text-[17px] font-semibold leading-6 text-foreground">Sign in</h1>
            <p className="text-[13px] leading-5 text-muted-foreground">Use your FlowPanel admin credentials.</p>
          </div>
        </div>

        {setupRequired ? (
          <Alert variant="destructive" className="mb-4 rounded-md px-3 py-2">
            <AlertDescription>
              Admin credentials are not configured. Set FLOWPANEL_ADMIN_USERNAME and FLOWPANEL_ADMIN_PASSWORD, then
              restart FlowPanel.
            </AlertDescription>
          </Alert>
        ) : null}

        {error ? (
          <Alert variant="destructive" className="mb-4 rounded-md px-3 py-2">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}

        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="panel-auth-username">Username</Label>
            <Input
              id="panel-auth-username"
              name="username"
              autoComplete="username"
              autoFocus
              required
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="panel-auth-password">Password</Label>
            <PasswordInput
              id="panel-auth-password"
              name="password"
              autoComplete="current-password"
              required
            />
          </div>
        </div>

        <Button type="submit" className="mt-5 h-9 w-full">
          Sign in
        </Button>
      </form>
    </main>
  );
}
