import { useEffect, useState, type FormEvent } from "react";
import type { PM2CreateProcessInput, PM2Process } from "@/api/pm2";
import { LoaderCircle } from "@/components/icons/lucide-icons";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

type PM2ProcessDialogProps = {
  open: boolean;
  process?: PM2Process | null;
  submitting: boolean;
  error: string | null;
  onOpenChange: (open: boolean) => void;
  onSubmit: (input: PM2CreateProcessInput) => void;
};

function formatArguments(argumentsList: string[] | undefined) {
  return (argumentsList ?? []).map((argument) => (/^[\w@%+=:,./-]+$/.test(argument) ? argument : JSON.stringify(argument))).join(" ");
}

function parseArguments(value: string) {
  const result: string[] = [];
  let token = "";
  let quote = "";
  let started = false;

  for (let index = 0; index < value.length; index += 1) {
    const character = value[index];
    if (character === "\\") {
      index += 1;
      if (index >= value.length) throw new Error("Arguments cannot end with a backslash.");
      token += value[index];
      started = true;
    } else if (quote) {
      if (character === quote) quote = "";
      else token += character;
      started = true;
    } else if (character === "\"" || character === "'") {
      quote = character;
      started = true;
    } else if (/\s/.test(character)) {
      if (started) {
        result.push(token);
        token = "";
        started = false;
      }
    } else {
      token += character;
      started = true;
    }
  }
  if (quote) throw new Error("Arguments contain an unclosed quote.");
  if (started) result.push(token);
  return result;
}

export function PM2ProcessDialog({ open, process, submitting, error, onOpenChange, onSubmit }: PM2ProcessDialogProps) {
  const editing = Boolean(process);
  const [name, setName] = useState("");
  const [scriptPath, setScriptPath] = useState("");
  const [workingDirectory, setWorkingDirectory] = useState("");
  const [argumentsValue, setArgumentsValue] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setName(process?.name ?? "");
    setScriptPath(process?.script_path ?? "");
    setWorkingDirectory(process?.working_directory ?? "");
    setArgumentsValue(formatArguments(process?.arguments));
    setValidationError(null);
  }, [open, process]);

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!scriptPath.trim()) {
      setValidationError("Script path is required.");
      return;
    }
    try {
      onSubmit({
        name: name.trim() || undefined,
        script_path: scriptPath.trim(),
        working_directory: workingDirectory.trim() || undefined,
        arguments: parseArguments(argumentsValue),
      });
      setValidationError(null);
    } catch (parseError) {
      setValidationError(parseError instanceof Error ? parseError.message : "Arguments are invalid.");
    }
  }

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !submitting && onOpenChange(nextOpen)}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{editing ? "Edit PM2 process" : "Add PM2 process"}</DialogTitle>
          <DialogDescription>
            {editing ? "Update the launch command. Running processes restart with the new configuration." : "Start a script or executable and optionally pass command arguments."}
          </DialogDescription>
        </DialogHeader>

        <form className="space-y-4" onSubmit={handleSubmit}>
          {error || validationError ? (
            <div className="rounded-lg border border-[var(--app-danger)]/30 bg-[var(--app-danger-soft)] px-3 py-2 text-sm text-[var(--app-danger)]">
              {validationError || error}
            </div>
          ) : null}

          <div className="space-y-1.5">
            <Label htmlFor="pm2-process-name">Process name</Label>
            <Input id="pm2-process-name" value={name} onChange={(event) => setName(event.target.value)} placeholder="m365-bridge" disabled={submitting} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="pm2-process-script">Script or executable</Label>
            <Input id="pm2-process-script" value={scriptPath} onChange={(event) => setScriptPath(event.target.value)} placeholder="/var/www/app/server" disabled={submitting} autoFocus />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="pm2-process-arguments">Arguments</Label>
            <Input id="pm2-process-arguments" value={argumentsValue} onChange={(event) => setArgumentsValue(event.target.value)} placeholder="serve --port 8000" disabled={submitting} />
            <p className="text-xs text-[var(--app-text-muted)]">Quotes are supported for arguments containing spaces.</p>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="pm2-process-cwd">Working directory</Label>
            <Input id="pm2-process-cwd" value={workingDirectory} onChange={(event) => setWorkingDirectory(event.target.value)} placeholder="/var/www/app" disabled={submitting} />
          </div>

          <div className="flex justify-end gap-2 pt-1">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>Cancel</Button>
            <Button type="submit" disabled={submitting}>
              {submitting ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
              {editing ? "Save changes" : "Add process"}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
