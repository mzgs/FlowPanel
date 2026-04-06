import { LoaderCircle, RefreshCw, Trash2 } from "@/components/icons/tabler-icons";
import { Badge } from "@/components/ui/badge";
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
import { Textarea } from "@/components/ui/textarea";

type DomainGitHubDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  hostname: string;
  repositoryUrl: string;
  autoDeployOnPush: boolean;
  postFetchScript: string;
  hasSavedIntegration: boolean;
  saving: boolean;
  deploying: boolean;
  fieldErrors: Record<string, string>;
  error: string | null;
  feedback: string | null;
  dirty: boolean;
  onRepositoryUrlChange: (value: string) => void;
  onAutoDeployOnPushChange: (checked: boolean) => void;
  onPostFetchScriptChange: (value: string) => void;
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
  repositoryUrl,
  autoDeployOnPush,
  postFetchScript,
  hasSavedIntegration,
  saving,
  deploying,
  fieldErrors,
  error,
  feedback,
  dirty,
  onRepositoryUrlChange,
  onAutoDeployOnPushChange,
  onPostFetchScriptChange,
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

        <div className="flex items-center">
          <Badge
            variant="outline"
            className={
              hasSavedIntegration
                ? "border-[var(--app-ok)]/30 bg-[var(--app-ok-soft)] text-[var(--app-ok)]"
                : "border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] text-[var(--app-danger)]"
            }
          >
            GitHub {hasSavedIntegration ? "connected" : "not connected"}
          </Badge>
        </div>

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

          <div className="space-y-2">
            <Label htmlFor="github_post_fetch_script">After fetch script</Label>
            <Textarea
              id="github_post_fetch_script"
              value={postFetchScript}
              onChange={(event) => onPostFetchScriptChange(event.target.value)}
              placeholder="composer install --no-dev"
              className="min-h-28 resize-y"
              spellCheck={false}
              aria-invalid={fieldErrors.post_fetch_script ? true : undefined}
            />
            <p className="text-xs leading-5 text-[var(--app-text-muted)]">
              Runs inside the domain target directory after FlowPanel fetches and resets the
              repository.
            </p>
            <FieldError message={fieldErrors.post_fetch_script} />
          </div>

          <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3">
            <div className="flex items-start justify-between gap-3">
              <div className="space-y-1">
                <Label htmlFor="github_auto_deploy" className="text-sm font-medium">
                  Auto update on push
                </Label>
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
