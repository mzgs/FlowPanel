import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { login, setupAdmin, type AuthApiError, type AuthCredentials, type AuthSession } from "@/api/auth";
import { PasswordInput } from "@/components/password-input";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { FieldError } from "@/components/field-error";
import { ShieldCheck } from "@/components/icons/lucide-icons";

type AuthPageProps = {
  setupRequired: boolean;
};

const emptyForm: AuthCredentials = {
  username: "",
  password: "",
};

export function AuthPage({ setupRequired }: AuthPageProps) {
  const queryClient = useQueryClient();
  const [form, setForm] = useState(emptyForm);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const mutation = useMutation<AuthSession, AuthApiError, AuthCredentials>({
    mutationFn: setupRequired ? setupAdmin : login,
    onSuccess: (session) => {
      queryClient.setQueryData(["auth", "session"], session);
    },
    onError: (error) => {
      setFieldErrors(error.fieldErrors ?? {});
    },
  });

  const title = setupRequired ? "Create admin user" : "Sign in";
  const description = setupRequired
    ? "Set the first FlowPanel administrator."
    : "Use your FlowPanel admin credentials.";

  function updateField(field: keyof AuthCredentials, value: string) {
    setForm((current) => ({ ...current, [field]: value }));
    if (fieldErrors[field]) {
      setFieldErrors((current) => {
        const next = { ...current };
        delete next[field];
        return next;
      });
    }
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFieldErrors({});
    mutation.mutate({
      username: form.username.trim().toLowerCase(),
      password: form.password,
    });
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-[var(--app-bg)] px-4 py-6">
      <form
        onSubmit={handleSubmit}
        className="w-full max-w-[360px] rounded-lg border border-[var(--app-border)] bg-[var(--app-bg-2)] p-5 shadow-[var(--app-shadow)]"
      >
        <div className="mb-5 flex items-start gap-3">
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <ShieldCheck className="h-4 w-4" />
          </div>
          <div className="min-w-0">
            <h1 className="text-[17px] font-semibold leading-6 text-foreground">{title}</h1>
            <p className="text-[13px] leading-5 text-muted-foreground">{description}</p>
          </div>
        </div>

        {mutation.error && !mutation.error.fieldErrors ? (
          <Alert variant="destructive" className="mb-4 rounded-md px-3 py-2">
            <AlertDescription>{mutation.error.message}</AlertDescription>
          </Alert>
        ) : null}

        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="panel-auth-username">Username</Label>
            <Input
              id="panel-auth-username"
              autoComplete="username"
              autoFocus
              value={form.username}
              onChange={(event) => updateField("username", event.target.value)}
              aria-invalid={fieldErrors.username ? true : undefined}
            />
            <FieldError message={fieldErrors.username} />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="panel-auth-password">Password</Label>
            <PasswordInput
              id="panel-auth-password"
              autoComplete={setupRequired ? "new-password" : "current-password"}
              value={form.password}
              onChange={(event) => updateField("password", event.target.value)}
              aria-invalid={fieldErrors.password ? true : undefined}
            />
            <FieldError message={fieldErrors.password} />
          </div>
        </div>

        <Button type="submit" className="mt-5 h-9 w-full" disabled={mutation.isPending}>
          {mutation.isPending ? "Working..." : title}
        </Button>
      </form>
    </main>
  );
}
