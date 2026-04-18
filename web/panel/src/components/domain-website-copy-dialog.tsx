import { type DomainRecord } from "@/api/domains";
import { FieldError } from "@/components/field-error";
import { LoaderCircle } from "@/components/icons/tabler-icons";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

type DomainWebsiteCopyDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sourceDomain: DomainRecord;
  targets: DomainRecord[];
  targetHostname: string;
  replaceTargetFiles: boolean;
  copying: boolean;
  error: string | null;
  fieldErrors: Record<string, string>;
  onTargetHostnameChange: (value: string) => void;
  onReplaceTargetFilesChange: (checked: boolean) => void;
  onCopy: () => void;
};

export function DomainWebsiteCopyDialog({
  open,
  onOpenChange,
  sourceDomain,
  targets,
  targetHostname,
  replaceTargetFiles,
  copying,
  error,
  fieldErrors,
  onTargetHostnameChange,
  onReplaceTargetFilesChange,
  onCopy,
}: DomainWebsiteCopyDialogProps) {
  const copyDisabled = copying || targetHostname.trim().length === 0 || targets.length === 0;
  const replaceTargetFilesCheckboxId = "website-copy-replace-target-files";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="gap-4 sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Copy website files</DialogTitle>
          <DialogDescription>
            Copy the current site contents into another existing domain.
          </DialogDescription>
        </DialogHeader>

        {error ? (
          <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-4 text-[13px] text-[var(--app-danger)]">
            {error}
          </div>
        ) : null}

        <section className="grid gap-3 border-b border-[var(--app-border)] pb-4 sm:grid-cols-2">
          <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3">
            <div className="text-xs text-[var(--app-text-muted)]">Source domain</div>
            <div className="mt-1 text-sm font-medium text-[var(--app-text)]">
              {sourceDomain.hostname}
            </div>
            <div className="mt-1 text-xs text-[var(--app-text-muted)]">{sourceDomain.kind}</div>
          </div>
          <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3">
            <div className="text-xs text-[var(--app-text-muted)]">Copy mode</div>
            <div className="mt-1 text-sm font-medium text-[var(--app-text)]">
              {replaceTargetFiles ? "Replace target files" : "Merge without replacing"}
            </div>
            <div className="mt-1 text-xs text-[var(--app-text-muted)]">
              Domain settings and runtime type are not changed.
            </div>
          </div>
        </section>

        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="website_copy_target_hostname">Destination domain</Label>
            <Select
              value={targetHostname}
              onValueChange={onTargetHostnameChange}
              disabled={copying || targets.length === 0}
            >
              <SelectTrigger
                id="website_copy_target_hostname"
                className="w-full"
                aria-invalid={fieldErrors.target_hostname ? true : undefined}
              >
                <SelectValue
                  placeholder={
                    targets.length === 0
                      ? "No other domains available"
                      : "Select destination domain"
                  }
                />
              </SelectTrigger>
              <SelectContent>
                {targets.map((target) => (
                  <SelectItem key={target.id} value={target.hostname}>
                    {target.hostname} · {target.kind}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldError message={fieldErrors.target_hostname} />
          </div>

          <div className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-3">
            <div className="flex items-start gap-3">
              <Checkbox
                id={replaceTargetFilesCheckboxId}
                checked={replaceTargetFiles}
                onCheckedChange={(checked) => onReplaceTargetFilesChange(checked === true)}
                disabled={copying}
              />
              <div className="space-y-1">
                <Label
                  htmlFor={replaceTargetFilesCheckboxId}
                  className="text-sm font-medium text-[var(--app-text)]"
                >
                  Replace target files
                </Label>
                <p className="text-xs leading-5 text-[var(--app-text-muted)]">
                  Clears the destination document root before copying. Disable this only if you
                  want a non-destructive merge and are sure file names will not collide.
                </p>
              </div>
            </div>
          </div>
        </div>

        <DialogFooter className="border-t border-[var(--app-border)] pt-4">
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={copying}>
            Cancel
          </Button>
          <Button type="button" onClick={onCopy} disabled={copyDisabled}>
            {copying ? (
              <>
                <LoaderCircle className="h-4 w-4 animate-spin" />
                Copying
              </>
            ) : (
              "Copy website"
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
