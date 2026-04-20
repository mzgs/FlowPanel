import { type DomainKind, type EnvironmentVariable } from "@/api/domains";
import { FieldError } from "@/components/field-error";
import { EnvironmentVariablesEditor } from "@/components/environment-variables-editor";
import { LoaderCircle } from "@/components/icons/tabler-icons";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

type DomainEnvironmentDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  hostname: string;
  kind: DomainKind;
  variables: EnvironmentVariable[];
  fieldErrors: Record<string, string>;
  error: string | null;
  saving: boolean;
  dirty: boolean;
  onAdd: () => void;
  onRemove: (index: number) => void;
  onKeyChange: (index: number, value: string) => void;
  onValueChange: (index: number, value: string) => void;
  onSave: () => void;
};

function getEnvironmentDescription(kind: DomainKind) {
  if (kind === "Php site") {
    return "Variables are passed through Caddy to PHP-FPM and apply after save. Values are stored in plain text.";
  }
  return "Variables are stored in plain text. Running runtimes are recreated after save so the new values apply immediately.";
}

export function DomainEnvironmentDialog({
  open,
  onOpenChange,
  hostname,
  kind,
  variables,
  fieldErrors,
  error,
  saving,
  dirty,
  onAdd,
  onRemove,
  onKeyChange,
  onValueChange,
  onSave,
}: DomainEnvironmentDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="gap-4 sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{hostname} Environment</DialogTitle>
          <DialogDescription>{getEnvironmentDescription(kind)}</DialogDescription>
        </DialogHeader>

        {error ? (
          <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-4 text-[13px] text-[var(--app-danger)]">
            {error}
          </div>
        ) : null}

        <EnvironmentVariablesEditor
          title="Variables"
          description="Add only plain-text values you want the app runtime to receive."
          variables={variables}
          fieldErrors={fieldErrors}
          fieldNamePrefix="environment_variables"
          inputIdPrefix="domain_env"
          emptyMessage="No environment variables saved for this domain."
          maxVariables={100}
          disabled={saving}
          onAdd={onAdd}
          onRemove={onRemove}
          onKeyChange={onKeyChange}
          onValueChange={onValueChange}
        />

        <DialogFooter className="border-t border-[var(--app-border)] pt-4">
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            Cancel
          </Button>
          <Button type="button" onClick={onSave} disabled={saving || !dirty}>
            {saving ? (
              <>
                <LoaderCircle className="h-4 w-4 animate-spin" />
                Saving
              </>
            ) : (
              "Save variables"
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
