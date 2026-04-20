import { useEffect, useRef } from "react";
import { type EnvironmentVariable } from "@/api/domains";
import { FieldError } from "@/components/field-error";
import { Plus, Trash2 } from "@/components/icons/tabler-icons";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

type EnvironmentVariablesEditorProps = {
  title: string;
  description: string;
  variables: EnvironmentVariable[];
  fieldErrors: Record<string, string>;
  fieldNamePrefix: string;
  inputIdPrefix: string;
  emptyMessage: string;
  addLabel?: string;
  maxVariables?: number;
  disabled?: boolean;
  onAdd: () => void;
  onRemove: (index: number) => void;
  onKeyChange: (index: number, value: string) => void;
  onValueChange: (index: number, value: string) => void;
};

export function EnvironmentVariablesEditor({
  title,
  description,
  variables,
  fieldErrors,
  fieldNamePrefix,
  inputIdPrefix,
  emptyMessage,
  addLabel = "Add variable",
  maxVariables,
  disabled = false,
  onAdd,
  onRemove,
  onKeyChange,
  onValueChange,
}: EnvironmentVariablesEditorProps) {
  const maxVariablesReached = typeof maxVariables === "number" && variables.length >= maxVariables;
  const pendingFocusIndexRef = useRef<number | null>(null);
  const keyInputRefs = useRef<Record<number, HTMLInputElement | null>>({});

  useEffect(() => {
    const pendingIndex = pendingFocusIndexRef.current;
    if (pendingIndex == null || pendingIndex >= variables.length) {
      return;
    }

    const input = keyInputRefs.current[pendingIndex];
    if (!input) {
      return;
    }

    pendingFocusIndexRef.current = null;
    requestAnimationFrame(() => {
      input.scrollIntoView({ behavior: "smooth", block: "center" });
      input.focus();
    });
  }, [variables.length]);

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <div className="text-sm font-medium text-[var(--app-text)]">{title}</div>
          <div className="text-xs text-[var(--app-text-muted)]">{description}</div>
        </div>
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            pendingFocusIndexRef.current = variables.length;
            onAdd();
          }}
          disabled={disabled || maxVariablesReached}
        >
          <Plus className="h-4 w-4" />
          {addLabel}
        </Button>
      </div>

      <FieldError message={fieldErrors[fieldNamePrefix]} />

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
                <td colSpan={3} className="px-4 py-8 text-center text-sm text-[var(--app-text-muted)]">
                  {emptyMessage}
                </td>
              </tr>
            ) : (
              variables.map((variable, index) => {
                const keyError = fieldErrors[`${fieldNamePrefix}[${index}].key`];
                const valueError = fieldErrors[`${fieldNamePrefix}[${index}].value`];

                return (
                  <tr key={index} className="align-top">
                    <td className="px-3 py-2">
                      <div className="space-y-2">
                        <Input
                          id={`${inputIdPrefix}_key_${index}`}
                          ref={(element) => {
                            keyInputRefs.current[index] = element;
                          }}
                          value={variable.key}
                          onChange={(event) => onKeyChange(index, event.target.value)}
                          placeholder="APP_ENV"
                          spellCheck={false}
                          autoComplete="off"
                          disabled={disabled}
                          aria-label={`Environment variable key ${index + 1}`}
                          aria-invalid={keyError ? true : undefined}
                        />
                        <FieldError message={keyError} />
                      </div>
                    </td>

                    <td className="px-3 py-2">
                      <div className="space-y-2">
                        <Input
                          id={`${inputIdPrefix}_value_${index}`}
                          value={variable.value}
                          onChange={(event) => onValueChange(index, event.target.value)}
                          placeholder="production"
                          spellCheck={false}
                          autoComplete="off"
                          disabled={disabled}
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
                        disabled={disabled}
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
  );
}
