import { Link } from "@tanstack/react-router";
import { GitBranch, LoaderCircle, RefreshCw, Trash2 } from "@/components/icons/tabler-icons";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";

type DomainGitHubDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  hostname: string;
  projectPath: string;
  repositoryUrl: string;
  autoDeployOnPush: boolean;
  defaultBranch: string;
  hasSavedIntegration: boolean;
  saving: boolean;
  deploying: boolean;
  fieldErrors: Record<string, string>;
  error: string | null;
  feedback: string | null;
  dirty: boolean;
  onRepositoryUrlChange: (value: string) => void;
  onAutoDeployOnPushChange: (checked: boolean) => void;
  onSave: () => void;
  onDeploy: () => void;
  onDisconnect: () => void;
};

function FieldError({ message }: { message?: string }) {
  if (!message) {
    return null;
  }

  return <p className="text-sm text-destructive">{message}</p>;
}

export function DomainGitHubDialog({
  open,
  onOpenChange,
  hostname,
  projectPath,
  repositoryUrl,
  autoDeployOnPush,
  defaultBranch,
  hasSavedIntegration,
  saving,
  deploying,
  fieldErrors,
  error,
  feedback,
  dirty,
  onRepositoryUrlChange,
  onAutoDeployOnPushChange,
  onSave,
  onDeploy,
  onDisconnect,
}: DomainGitHubDialogProps) {
  const busy = saving || deploying;
  const canDeploy = repositoryUrl.trim().length > 0 && !busy;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="gap-4 sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{hostname} GitHub</DialogTitle>
          <DialogDescription>
            Connect a repository, optionally deploy on push, and trigger a manual update when
            needed.
          </DialogDescription>
        </DialogHeader>

        {error ? (
          <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-4 text-[13px] text-[var(--app-danger)]">
            {error}
          </div>
        ) : null}
        {!error && feedback ? (
          <div className="rounded-lg border border-emerald-500/20 bg-emerald-500/8 px-3 py-4 text-[13px] text-emerald-700 dark:text-emerald-300">
            {feedback}
          </div>
        ) : null}

        <section className="grid gap-4 rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] p-4 md:grid-cols-[minmax(0,1fr)_220px]">
          <div className="space-y-1.5">
            <p className="text-sm font-semibold text-[var(--app-text)]">Deployment target</p>
            <p className="break-all font-mono text-xs text-[var(--app-text-muted)]">
              {projectPath}
            </p>
            <p className="text-xs leading-5 text-[var(--app-text-muted)]">
              The panel uses the GitHub token stored in{" "}
              <Link
                to="/settings"
                className="font-medium text-[var(--app-text)] underline underline-offset-2"
              >
                Settings
              </Link>{" "}
              for repository access and webhook setup.
            </p>
          </div>
          <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface)] px-3 py-3">
            <div className="flex items-center gap-2 text-sm font-semibold text-[var(--app-text)]">
              <GitBranch className="h-4 w-4 text-[var(--app-text-muted)]" stroke={1.8} />
              GitHub state
            </div>
            <div className="mt-3 space-y-2 text-xs text-[var(--app-text-muted)]">
              <p>{hasSavedIntegration ? "Repository connected" : "No repository connected"}</p>
              <p>
                {autoDeployOnPush
                  ? `Pushes to ${defaultBranch || "the default branch"} deploy automatically.`
                  : "Auto update is off. Use Deploy now when you want to pull changes."}
              </p>
            </div>
          </div>
        </section>

        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="github_repository_url">Repository URL</Label>
            <Input
              id="github_repository_url"
              value={repositoryUrl}
              onChange={(event) => onRepositoryUrlChange(event.target.value)}
              placeholder="https://github.com/owner/repository"
              autoComplete="off"
              spellCheck={false}
              aria-invalid={fieldErrors.repository_url ? true : undefined}
            />
            <FieldError message={fieldErrors.repository_url} />
          </div>

          <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3">
            <div className="flex items-start justify-between gap-3">
              <div className="space-y-1">
                <Label htmlFor="github_auto_deploy" className="text-sm font-medium">
                  Auto update on push
                </Label>
                <p className="text-xs leading-5 text-[var(--app-text-muted)]">
                  When enabled, FlowPanel registers a GitHub webhook and deploys pushes from the
                  default branch automatically.
                </p>
              </div>
              <Switch
                id="github_auto_deploy"
                checked={autoDeployOnPush}
                onCheckedChange={onAutoDeployOnPushChange}
                disabled={busy || repositoryUrl.trim().length === 0}
              />
            </div>
          </div>
        </div>

        <DialogFooter className="border-t border-[var(--app-border)] pt-4">
          {hasSavedIntegration ? (
            <Button type="button" variant="outline" onClick={onDisconnect} disabled={busy}>
              {saving ? (
                <>
                  <LoaderCircle className="h-4 w-4 animate-spin" />
                  Disconnecting
                </>
              ) : (
                <>
                  <Trash2 className="h-4 w-4" />
                  Disconnect
                </>
              )}
            </Button>
          ) : (
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
              Cancel
            </Button>
          )}

          <div className="flex items-center gap-2">
            <Button type="button" variant="outline" onClick={onDeploy} disabled={!canDeploy}>
              {deploying ? (
                <>
                  <LoaderCircle className="h-4 w-4 animate-spin" />
                  Deploying
                </>
              ) : (
                <>
                  <RefreshCw className="h-4 w-4" />
                  Deploy now
                </>
              )}
            </Button>
            <Button type="button" onClick={onSave} disabled={busy || !dirty}>
              {saving ? (
                <>
                  <LoaderCircle className="h-4 w-4 animate-spin" />
                  Saving
                </>
              ) : hasSavedIntegration ? (
                "Save changes"
              ) : (
                "Connect repository"
              )}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
