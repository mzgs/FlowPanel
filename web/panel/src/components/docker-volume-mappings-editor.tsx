import { useEffect, useRef } from "react";
import { type DockerContainerVolumeMapping } from "@/api/docker";
import { FieldError } from "@/components/field-error";
import { Plus, Trash2 } from "@/components/icons/tabler-icons";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

type DockerVolumeMappingsEditorProps = {
  title: string;
  description: string;
  volumes: DockerContainerVolumeMapping[];
  fieldErrors: Record<string, string>;
  fieldNamePrefix: string;
  inputIdPrefix: string;
  emptyMessage: string;
  addLabel?: string;
  disabled?: boolean;
  onAdd: () => void;
  onRemove: (index: number) => void;
  onSourceChange: (index: number, value: string) => void;
  onDestinationChange: (index: number, value: string) => void;
};

export function DockerVolumeMappingsEditor({
  title,
  description,
  volumes,
  fieldErrors,
  fieldNamePrefix,
  inputIdPrefix,
  emptyMessage,
  addLabel = "Add volume",
  disabled = false,
  onAdd,
  onRemove,
  onSourceChange,
  onDestinationChange,
}: DockerVolumeMappingsEditorProps) {
  const pendingFocusIndexRef = useRef<number | null>(null);
  const sourceInputRefs = useRef<Record<number, HTMLInputElement | null>>({});

  useEffect(() => {
    const pendingIndex = pendingFocusIndexRef.current;
    if (pendingIndex == null || pendingIndex >= volumes.length) {
      return;
    }

    const input = sourceInputRefs.current[pendingIndex];
    if (!input) {
      return;
    }

    pendingFocusIndexRef.current = null;
    requestAnimationFrame(() => {
      input.scrollIntoView({ behavior: "smooth", block: "center" });
      input.focus();
    });
  }, [volumes.length]);

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
            pendingFocusIndexRef.current = volumes.length;
            onAdd();
          }}
          disabled={disabled}
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
              <th className="w-[44%] px-3 py-2 font-medium">Source</th>
              <th className="w-[44%] px-3 py-2 font-medium">Container path</th>
              <th className="w-14 px-3 py-2 font-medium">
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {volumes.length === 0 ? (
              <tr>
                <td colSpan={3} className="px-4 py-8 text-center text-sm text-[var(--app-text-muted)]">
                  {emptyMessage}
                </td>
              </tr>
            ) : (
              volumes.map((volume, index) => {
                const sourceError = fieldErrors[`${fieldNamePrefix}[${index}].source`];
                const destinationError = fieldErrors[`${fieldNamePrefix}[${index}].destination`];

                return (
                  <tr key={index} className="align-top">
                    <td className="px-3 py-2">
                      <div className="space-y-2">
                        <Input
                          id={`${inputIdPrefix}_source_${index}`}
                          ref={(element) => {
                            sourceInputRefs.current[index] = element;
                          }}
                          value={volume.source}
                          onChange={(event) => onSourceChange(index, event.target.value)}
                          placeholder="/srv/data or app-data"
                          spellCheck={false}
                          autoComplete="off"
                          disabled={disabled}
                          aria-label={`Volume source ${index + 1}`}
                          aria-invalid={sourceError ? true : undefined}
                        />
                        <FieldError message={sourceError} />
                      </div>
                    </td>

                    <td className="px-3 py-2">
                      <div className="space-y-2">
                        <Input
                          id={`${inputIdPrefix}_destination_${index}`}
                          value={volume.destination}
                          onChange={(event) => onDestinationChange(index, event.target.value)}
                          placeholder="/data"
                          spellCheck={false}
                          autoComplete="off"
                          disabled={disabled}
                          aria-label={`Volume destination ${index + 1}`}
                          aria-invalid={destinationError ? true : undefined}
                        />
                        <FieldError message={destinationError} />
                      </div>
                    </td>

                    <td className="px-3 py-2">
                      <Button
                        type="button"
                        variant="outline"
                        size="icon"
                        onClick={() => onRemove(index)}
                        disabled={disabled}
                        aria-label={`Remove volume ${index + 1}`}
                        title="Remove volume"
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
