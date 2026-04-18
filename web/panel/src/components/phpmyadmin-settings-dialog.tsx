import { useRef, useState, type ChangeEvent } from "react";
import { importPHPMyAdminTheme, type PHPMyAdminStatus } from "@/api/phpmyadmin";
import { LoaderCircle, Upload } from "@/components/icons/tabler-icons";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { getErrorMessage } from "@/lib/utils";
import { Label } from "@/components/ui/label";
import { toast } from "sonner";

type PHPMyAdminSettingsDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  status: PHPMyAdminStatus | null;
  onStatusChange: (status: PHPMyAdminStatus) => void;
};

export function PHPMyAdminSettingsDialog({
  open,
  onOpenChange,
  status,
  onStatusChange,
}: PHPMyAdminSettingsDialogProps) {
  const importInputRef = useRef<HTMLInputElement | null>(null);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const installPath = status?.install_path ?? "";

  async function handleThemeSelection(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) {
      return;
    }

    setUploading(true);
    setError(null);

    try {
      const nextStatus = await importPHPMyAdminTheme(file);
      onStatusChange(nextStatus);
      toast.success("phpMyAdmin theme imported.");
    } catch (uploadError) {
      const message = getErrorMessage(uploadError, "Failed to import phpMyAdmin theme.");
      setError(message);
      toast.error(message);
    } finally {
      setUploading(false);
    }
  }

  return (
    <>
      <input
        ref={importInputRef}
        type="file"
        accept=".zip,application/zip,application/x-zip-compressed"
        className="hidden"
        onChange={(event) => {
          void handleThemeSelection(event);
        }}
      />

      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>phpMyAdmin settings</DialogTitle>
          </DialogHeader>

          <div className="space-y-5">
            {error ? (
              <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-2 text-sm text-[var(--app-danger)]">
                {error}
              </div>
            ) : null}

            <section className="space-y-2">
              <Label htmlFor="phpmyadmin_path">phpMyAdmin path</Label>
              <Badge
                id="phpmyadmin_path"
                variant="outline"
                className="max-w-full justify-start whitespace-normal break-all py-1 text-left"
              >
                {installPath || "Not installed"}
              </Badge>
            </section>

            <section className="rounded-lg border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-4">
              <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <div className="text-sm font-medium text-[var(--app-text)]">Import theme</div>
                  <p className="mt-1 text-xs text-[var(--app-text-muted)]">
                    Upload a ZIP archive. Matching theme folders or files in the target `themes` directory are replaced.
                  </p>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => importInputRef.current?.click()}
                  disabled={uploading || !status?.installed}
                  title={status?.installed ? undefined : "Install phpMyAdmin before importing a theme."}
                >
                  {uploading ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}
                  Import theme
                </Button>
              </div>
            </section>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={uploading}>
              Close
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
