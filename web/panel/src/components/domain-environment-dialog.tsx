import { type DomainKind, type EnvironmentVariable } from "@/api/domains";
import { FieldError } from "@/components/field-error";
import { LoaderCircle, Plus, Trash2 } from "@/components/icons/tabler-icons";
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
  const maxVariablesReached = variables.length >= 100;

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

        <div className="space-y-4">
          <div className="flex items-center justify-between gap-3">
            <div>
              <div className="text-sm font-medium text-[var(--app-text)]">Variables</div>
              <div className="text-xs text-[var(--app-text-muted)]">
                Add only plain-text values you want the app runtime to receive.
              </div>
            </div>
            <Button type="button" variant="outline" onClick={onAdd} disabled={saving || maxVariablesReached}>
              <Plus className="h-4 w-4" />
              Add variable
            </Button>
          </div>

          <FieldError message={fieldErrors.environment_variables} />

          <div className="overflow-x-auto rounded-xl border border-[var(--app-border)] bg-[var(--app-surface-muted)]">
            <table className="w-full table-fixed">
              <thead>
                <tr className="text-left text-xs font-medium tracking-[0.08em] text-[var(--app-text-muted)] uppercase">
                  <th className="px-3 py-2 font-medium">Key</th>
                  <th className="px-3 py-2 font-medium">Value</th>
                  <th className="w-14 px-3 py-2 font-medium">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {variables.length === 0 ? (
                  <tr>
                    <td
                      colSpan={3}
                      className="px-4 py-8 text-center text-sm text-[var(--app-text-muted)]"
                    >
                      No environment variables saved for this domain.
                    </td>
                  </tr>
                ) : (
                  variables.map((variable, index) => {
                    const keyError = fieldErrors[`environment_variables[${index}].key`];
                    const valueError = fieldErrors[`environment_variables[${index}].value`];

                    return (
                      <tr key={index} className="align-top">
                        <td className="px-3 py-2">
                          <div className="space-y-2">
                            <Input
                              id={`domain_env_key_${index}`}
                              value={variable.key}
                              onChange={(event) => onKeyChange(index, event.target.value)}
                              placeholder="APP_ENV"
                              spellCheck={false}
                              autoComplete="off"
                              disabled={saving}
                              aria-label={`Environment variable key ${index + 1}`}
                              aria-invalid={keyError ? true : undefined}
                            />
                            <FieldError message={keyError} />
                          </div>
                        </td>

                        <td className="px-3 py-2">
                          <div className="space-y-2">
                            <Input
                              id={`domain_env_value_${index}`}
                              value={variable.value}
                              onChange={(event) => onValueChange(index, event.target.value)}
                              placeholder="production"
                              spellCheck={false}
                              autoComplete="off"
                              disabled={saving}
                              aria-label={`Environment variable value ${index + 1}`}
                              aria-invalid={valueError ? true : undefined}
                            />
                            <FieldError message={valueError} />
                          </div>
                        </td>

                        <td className="px-3 py-2">
                          <Button
                            type="button"
                            variant="outline"
                            size="icon"
                            onClick={() => onRemove(index)}
                            disabled={saving}
                            aria-label={`Remove environment variable ${index + 1}`}
                            title="Remove variable"
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </td>
                      </tr>
                    );
                  })
                )}
              </tbody>
            </table>
          </div>
        </div>

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
