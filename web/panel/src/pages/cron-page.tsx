import { useEffect, useState, type FormEvent } from "react";
import {
  createCronJob,
  deleteCronJob,
  type CronExecutionLog,
  fetchCronJobs,
  type CronPayload,
  runCronJob,
  type CronApiError,
  type CronJob,
  updateCronJob,
} from "@/api/cron";
import {
  Clock,
  Copy,
  LoaderCircle,
  Pencil,
  PlayerPlay,
  RefreshCw,
  TerminalSquare,
  Trash2,
} from "@/components/icons/tabler-icons";
import { ActionConfirmDialog } from "@/components/action-confirm-dialog";
import { PageHeader } from "@/components/page-header";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { formatDateTime } from "@/lib/format";
import { cn, copyTextToClipboard, sleep } from "@/lib/utils";
import { toast } from "sonner";

type FormState = {
  name: string;
  schedule: string;
  command: string;
};

type FormErrors = {
  name?: string;
  schedule?: string;
  command?: string;
};

const initialForm: FormState = {
  name: "",
  schedule: "",
  command: "",
};

function getErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error && error.message) {
    return error.message;
  }

  return fallback;
}

function getSchedulerBadge(enabled: boolean, started: boolean) {
  if (!enabled) {
    return {
      label: "Saved only",
      variant: "outline" as const,
    };
  }

  if (!started) {
    return {
      label: "Starting",
      variant: "outline" as const,
    };
  }

  return {
    label: "Running",
    variant: "secondary" as const,
  };
}

function normalizeJobs(jobs: CronJob[] | null | undefined) {
  if (!Array.isArray(jobs)) {
    return [];
  }

  return jobs.map((job) => ({
    ...job,
    executions: Array.isArray(job.executions) ? job.executions : [],
  }));
}

function formatDuration(durationMs: number) {
  if (durationMs < 1000) {
    return `${durationMs} ms`;
  }

  const seconds = durationMs / 1000;
  if (seconds < 60) {
    return `${seconds >= 10 ? seconds.toFixed(0) : seconds.toFixed(1)} s`;
  }

  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = Math.round(seconds % 60);
  if (remainingSeconds === 0) {
    return `${minutes} min`;
  }

  return `${minutes} min ${remainingSeconds}s`;
}

function getExecutionBadge(execution: CronExecutionLog) {
  if (execution.status === "failed") {
    return {
      label: "Failed",
      variant: "destructive" as const,
    };
  }

  return {
    label: "Succeeded",
    variant: "secondary" as const,
  };
}

function sortExecutions(executions: CronExecutionLog[]) {
  return [...executions].sort(
    (left, right) => new Date(right.started_at).getTime() - new Date(left.started_at).getTime(),
  );
}

function getExecutionPreview(execution: CronExecutionLog) {
  const previewSource = execution.error.trim() || execution.output.trim();
  if (!previewSource) {
    return "No output captured.";
  }

  const [firstLine] = previewSource.split(/\r?\n/, 1);
  return firstLine;
}

export function CronPage() {
  const [jobs, setJobs] = useState<CronJob[]>([]);
  const [enabled, setEnabled] = useState(false);
  const [started, setStarted] = useState(false);
  const [form, setForm] = useState<FormState>(initialForm);
  const [errors, setErrors] = useState<FormErrors>({});
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [editingJobId, setEditingJobId] = useState<string | null>(null);
  const [runningJobId, setRunningJobId] = useState<string | null>(null);
  const [deletingJobId, setDeletingJobId] = useState<string | null>(null);
  const [deleteJobCandidate, setDeleteJobCandidate] = useState<CronJob | null>(null);
  const [logsJobId, setLogsJobId] = useState<string | null>(null);
  const [selectedExecutionId, setSelectedExecutionId] = useState<string | null>(null);
  const schedulerBadge = getSchedulerBadge(enabled, started);
  const isEditing = editingJobId !== null;
  const logsJob = jobs.find((job) => job.id === logsJobId) ?? null;
  const sortedExecutions = logsJob ? sortExecutions(logsJob.executions) : [];
  const executionsSignature = logsJob ? logsJob.executions.map((execution) => execution.id).join(":") : "";
  const selectedExecution = sortedExecutions.find((execution) => execution.id === selectedExecutionId) ?? sortedExecutions[0] ?? null;
  const selectedExecutionBadge = selectedExecution ? getExecutionBadge(selectedExecution) : null;
  const selectedExecutionOutputLineCount = selectedExecution?.output ? selectedExecution.output.split(/\r?\n/).length : 0;

  function resetForm() {
    setForm(initialForm);
    setErrors({});
    setFormError(null);
    setEditingJobId(null);
  }

  function syncPayload(payload: CronPayload, preferredJobId?: string | null) {
    const nextJobs = normalizeJobs(payload.jobs);

    setJobs(nextJobs);
    setEnabled(payload.enabled);
    setStarted(payload.started);
    setLogsJobId((currentLogsJobId) => {
      const desiredJobId = preferredJobId ?? currentLogsJobId;
      if (desiredJobId && nextJobs.some((job) => job.id === desiredJobId)) {
        return desiredJobId;
      }

      return currentLogsJobId === null ? null : nextJobs[0]?.id ?? null;
    });
  }

  async function refreshExecutions(jobId: string, previousExecutionCount: number) {
    for (let attempt = 0; attempt < 6; attempt += 1) {
      await sleep(400);

      try {
        const payload = await fetchCronJobs();
        const nextJobs = normalizeJobs(payload.jobs);

        syncPayload(payload, jobId);

        const nextJob = nextJobs.find((currentJob) => currentJob.id === jobId);
        if ((nextJob?.executions.length ?? 0) > previousExecutionCount) {
          return;
        }
      } catch {
        return;
      }
    }
  }

  useEffect(() => {
    let active = true;

    async function loadJobs() {
      try {
        const payload = await fetchCronJobs();
        if (!active) {
          return;
        }

        syncPayload(payload);
        setLoadError(null);
      } catch (error) {
        if (!active) {
          return;
        }

        setLoadError(getErrorMessage(error, "Failed to load cron jobs."));
      } finally {
        if (active) {
          setLoading(false);
        }
      }
    }

    void loadJobs();

    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (!logsJob) {
      setSelectedExecutionId(null);
      return;
    }

    setSelectedExecutionId((currentExecutionId) => {
      if (currentExecutionId && sortedExecutions.some((execution) => execution.id === currentExecutionId)) {
        return currentExecutionId;
      }

      return sortedExecutions[0]?.id ?? null;
    });
  }, [logsJobId, executionsSignature]);

  async function handleRefresh() {
    setLoading(true);

    try {
      const payload = await fetchCronJobs();
      syncPayload(payload);
      setLoadError(null);
    } catch (error) {
      setLoadError(getErrorMessage(error, "Failed to load cron jobs."));
    } finally {
      setLoading(false);
    }
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    const nextErrors: FormErrors = {};
    if (!form.name.trim()) {
      nextErrors.name = "Name is required.";
    }
    if (!form.schedule.trim()) {
      nextErrors.schedule = "Schedule is required.";
    }
    if (!form.command.trim()) {
      nextErrors.command = "Command is required.";
    }

    setErrors(nextErrors);
    setFormError(null);
    if (Object.keys(nextErrors).length > 0) {
      return;
    }

    setSubmitting(true);

    try {
      const input = {
        name: form.name.trim(),
        schedule: form.schedule.trim(),
        command: form.command.trim(),
      };

      if (isEditing && editingJobId) {
        const updatedJob = await updateCronJob(editingJobId, input);
        setJobs((currentJobs) =>
          currentJobs.map((currentJob) => (currentJob.id === updatedJob.id ? updatedJob : currentJob)),
        );
      } else {
        const createdJob = await createCronJob(input);
        setJobs((currentJobs) => [createdJob, ...currentJobs]);
      }

      setLoadError(null);
      resetForm();
    } catch (error) {
      const apiError = error as CronApiError;
      setErrors(apiError.fieldErrors ?? {});
      setFormError(getErrorMessage(error, isEditing ? "Failed to update cron job." : "Failed to create cron job."));
    } finally {
      setSubmitting(false);
    }
  }

  function handleEdit(job: CronJob) {
    setForm({
      name: job.name,
      schedule: job.schedule,
      command: job.command,
    });
    setErrors({});
    setFormError(null);
    setLoadError(null);
    setEditingJobId(job.id);
  }

  async function handleRun(job: CronJob) {
    setRunningJobId(job.id);
    setLoadError(null);

    try {
      await runCronJob(job.id);
      toast.success(`Started "${job.name}".`);
      void refreshExecutions(job.id, job.executions.length);
    } catch (error) {
      setLoadError(getErrorMessage(error, `Failed to run ${job.name}.`));
      toast.error(`Failed to start "${job.name}".`);
    } finally {
      setRunningJobId(null);
    }
  }

  function handleDelete(job: CronJob) {
    if (deletingJobId !== null) {
      return;
    }

    setDeleteJobCandidate(job);
  }

  async function confirmDeleteJob() {
    if (!deleteJobCandidate) {
      return;
    }

    const job = deleteJobCandidate;
    setDeletingJobId(job.id);

    try {
      await deleteCronJob(job.id);
      const nextJobs = jobs.filter((currentJob) => currentJob.id !== job.id);
      setJobs(nextJobs);
      if (logsJobId === job.id) {
        setLogsJobId(null);
      }
      if (editingJobId === job.id) {
        resetForm();
      }
    } catch (error) {
      setLoadError(getErrorMessage(error, "Failed to delete cron job."));
    } finally {
      setDeletingJobId(null);
      setDeleteJobCandidate((current) => (current?.id === job.id ? null : current));
    }
  }

  async function handleCopyExecutionOutput(execution: CronExecutionLog) {
    if (!execution.output) {
      return;
    }

    try {
      await copyTextToClipboard(execution.output);
      toast.success("Execution output copied.");
    } catch {
      toast.error("Failed to copy execution output.");
    }
  }

  return (
    <>
      <PageHeader
        title="Cron"
        meta="Add local scheduled commands and keep the saved job list in sync with the server."
        actions={
          <Button type="button" variant="outline" onClick={() => void handleRefresh()} disabled={loading}>
            {loading ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
            Refresh
          </Button>
        }
      />
      <ActionConfirmDialog
        open={deleteJobCandidate !== null}
        onOpenChange={(open) => {
          if (!open && deletingJobId === null) {
            setDeleteJobCandidate(null);
          }
        }}
        title="Delete cron job"
        desc={deleteJobCandidate ? `Delete cron job "${deleteJobCandidate.name}"?` : "Delete this cron job?"}
        confirmText="Delete cron job"
        destructive
        isLoading={deleteJobCandidate !== null && deletingJobId === deleteJobCandidate.id}
        handleConfirm={() => {
          void confirmDeleteJob();
        }}
        className="sm:max-w-md"
      />

      <div className="space-y-6 px-4 pb-8 sm:px-6 lg:px-8">
        <section className="rounded-lg border bg-card">
          <div className="flex flex-col gap-3 px-4 py-3 lg:flex-row lg:items-center lg:justify-between">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant={schedulerBadge.variant}>{schedulerBadge.label}</Badge>
              <span className="text-sm text-muted-foreground">
                {jobs.length} {jobs.length === 1 ? "job" : "jobs"} saved
              </span>
            </div>
            <div className="text-sm text-muted-foreground">
              Supports standard 5-field cron expressions and descriptors like `@daily`.
            </div>
          </div>
        </section>

        {!enabled ? (
          <Alert>
            <Clock className="h-4 w-4" />
            <AlertTitle>Scheduler is disabled</AlertTitle>
            <AlertDescription>
              Jobs are still saved here, but they will not run until `FLOWPANEL_CRON_ENABLED` is enabled and the
              server is restarted.
            </AlertDescription>
          </Alert>
        ) : null}

        {loadError ? (
          <Alert variant="destructive">
            <TerminalSquare className="h-4 w-4" />
            <AlertTitle>Unable to load cron jobs</AlertTitle>
            <AlertDescription>{loadError}</AlertDescription>
          </Alert>
        ) : null}

        <div className="grid gap-6 xl:grid-cols-[360px_minmax(0,1fr)]">
          <section className="rounded-lg border bg-card">
            <div className="border-b px-4 py-4">
              <h2 className="text-base font-semibold tracking-tight">{isEditing ? "Edit job" : "New job"}</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                {isEditing
                  ? "Update the saved schedule or command, then save it back to the server."
                  : "Save a schedule, label it clearly, and run any shell command available on this server."}
              </p>
            </div>

            <form className="space-y-4 p-4" onSubmit={handleSubmit}>
              <div className="space-y-2">
                <label htmlFor="cron-name" className="text-sm font-medium">
                  Name
                </label>
                <Input
                  id="cron-name"
                  value={form.name}
                  onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))}
                  placeholder="Nightly backup"
                  aria-invalid={errors.name ? "true" : undefined}
                />
                {errors.name ? <p className="text-sm text-destructive">{errors.name}</p> : null}
              </div>

              <div className="space-y-2">
                <label htmlFor="cron-schedule" className="text-sm font-medium">
                  Schedule
                </label>
                <Input
                  id="cron-schedule"
                  value={form.schedule}
                  onChange={(event) => setForm((current) => ({ ...current, schedule: event.target.value }))}
                  placeholder="0 3 * * *"
                  spellCheck={false}
                  aria-invalid={errors.schedule ? "true" : undefined}
                />
                <p className="text-sm text-muted-foreground">Examples: `*/15 * * * *`, `0 3 * * *`, `@daily`.</p>
                {errors.schedule ? <p className="text-sm text-destructive">{errors.schedule}</p> : null}
              </div>

              <div className="space-y-2">
                <label htmlFor="cron-command" className="text-sm font-medium">
                  Command
                </label>
                <Textarea
                  id="cron-command"
                  value={form.command}
                  onChange={(event) => setForm((current) => ({ ...current, command: event.target.value }))}
                  placeholder="cd /var/www/example && php artisan schedule:run"
                  spellCheck={false}
                  className="min-h-28 font-mono text-sm"
                  aria-invalid={errors.command ? "true" : undefined}
                />
                {errors.command ? <p className="text-sm text-destructive">{errors.command}</p> : null}
              </div>

              {formError ? <p className="text-sm text-destructive">{formError}</p> : null}

              <div className="flex gap-3">
                <Button type="submit" className="flex-1" disabled={submitting}>
                  {submitting ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
                  {isEditing ? "Save changes" : "Add job"}
                </Button>
                {isEditing ? (
                  <Button type="button" variant="outline" onClick={resetForm} disabled={submitting}>
                    Cancel
                  </Button>
                ) : null}
              </div>
            </form>
          </section>

          <section className="rounded-lg border bg-card">
            <div className="border-b px-4 py-4">
              <h2 className="text-base font-semibold tracking-tight">Saved jobs</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                Each saved entry is persisted in the panel database and registered with the scheduler when it is
                running.
              </p>
            </div>

            {loading ? (
              <div className="flex items-center gap-2 px-4 py-10 text-sm text-muted-foreground">
                <LoaderCircle className="h-4 w-4 animate-spin" />
                Loading cron jobs...
              </div>
            ) : jobs.length === 0 ? (
              <div className="px-4 py-10 text-sm text-muted-foreground">No cron jobs have been added yet.</div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead>Schedule</TableHead>
                    <TableHead>Command</TableHead>
                    <TableHead>Added</TableHead>
                    <TableHead className="w-[196px] text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {jobs.map((job) => (
                    <TableRow key={job.id} className="align-top">
                      <TableCell className="font-medium">
                        <div className="space-y-1">
                          <div>{job.name}</div>
                          <div className="text-xs font-normal text-muted-foreground">
                            {job.executions.length} {job.executions.length === 1 ? "execution" : "executions"}
                          </div>
                        </div>
                      </TableCell>
                      <TableCell className="font-mono text-xs">{job.schedule}</TableCell>
                      <TableCell className="max-w-[28rem] whitespace-normal break-all font-mono text-xs text-muted-foreground">
                        {job.command}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">{formatDateTime(job.created_at)}</TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end gap-1">
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            onClick={() => void handleRun(job)}
                            disabled={runningJobId === job.id}
                            aria-label={`Run ${job.name} now`}
                            title="Run now"
                          >
                            {runningJobId === job.id ? (
                              <LoaderCircle className="h-4 w-4 animate-spin" />
                            ) : (
                              <PlayerPlay className="h-4 w-4" />
                            )}
                          </Button>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            onClick={() => handleEdit(job)}
                            aria-label={`Edit ${job.name}`}
                            title="Edit"
                          >
                            <Pencil className="h-4 w-4" />
                          </Button>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            onClick={() => setLogsJobId(job.id)}
                            aria-label={`Show logs for ${job.name}`}
                            title="Logs"
                          >
                            <TerminalSquare className="h-4 w-4" />
                          </Button>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            onClick={() => handleDelete(job)}
                            disabled={deletingJobId === job.id}
                            aria-label={`Delete ${job.name}`}
                            title="Delete"
                          >
                            {deletingJobId === job.id ? (
                              <LoaderCircle className="h-4 w-4 animate-spin" />
                            ) : (
                              <Trash2 className="h-4 w-4" />
                            )}
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </section>
        </div>
      </div>

      <Dialog open={logsJob !== null} onOpenChange={(open) => (!open ? setLogsJobId(null) : null)}>
        <DialogContent className="max-w-5xl overflow-hidden p-0 sm:max-w-5xl">
          <DialogHeader className="border-b border-[var(--app-border)] bg-[var(--app-surface)] px-6 py-5">
            <DialogTitle>{logsJob ? `${logsJob.name} logs` : "Execution logs"}</DialogTitle>
            <DialogDescription>
              {logsJob
                ? `${sortedExecutions.length} ${sortedExecutions.length === 1 ? "execution" : "executions"} recorded`
                : "Recent cron job execution output."}
            </DialogDescription>
          </DialogHeader>

          <div className="bg-[var(--app-surface)] text-[var(--app-text)]">
            {logsJob ? (
              sortedExecutions.length === 0 ? (
                <div className="px-6 py-6">
                  <div className="rounded-md border border-dashed border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 py-10 text-sm text-[var(--app-text-muted)]">
                    No executions recorded for this job yet.
                  </div>
                </div>
              ) : (
                <div className="grid min-h-[28rem] bg-[var(--app-surface)] lg:grid-cols-[300px_minmax(0,1fr)]">
                  <aside className="border-b border-[var(--app-border)] bg-[var(--app-surface-muted)] lg:border-b-0 lg:border-r">
                    <div className="border-b border-[var(--app-border)] px-4 py-3">
                      <div className="font-mono text-xs text-[var(--app-text)]">{logsJob.schedule}</div>
                      <p className="mt-1 line-clamp-2 break-all font-mono text-xs text-[var(--app-text-muted)]">
                        {logsJob.command}
                      </p>
                    </div>

                    <div className="flex items-center justify-between border-b border-[var(--app-border)] px-4 py-3 text-xs text-[var(--app-text-muted)]">
                      <span>{sortedExecutions.length} runs</span>
                      <span>Latest first</span>
                    </div>

                    <ScrollArea className="h-[18rem] lg:h-[calc(28rem-85px)]">
                      <div className="space-y-2 p-2">
                        {sortedExecutions.map((execution) => {
                          const badge = getExecutionBadge(execution);
                          const isSelected = execution.id === selectedExecution?.id;

                          return (
                            <button
                              key={execution.id}
                              type="button"
                              onClick={() => setSelectedExecutionId(execution.id)}
                              className={cn(
                                "w-full rounded-md border px-3 py-3 text-left text-[var(--app-text)] transition-colors",
                                isSelected
                                  ? "border-[var(--app-border)] bg-[var(--app-surface-elev)] shadow-[var(--app-shadow)]"
                                  : "border-transparent bg-transparent hover:border-[var(--app-border)] hover:bg-[var(--app-surface)]",
                              )}
                            >
                              <div className="flex items-center justify-between gap-3">
                                <Badge variant={badge.variant}>{badge.label}</Badge>
                                <span className="text-xs text-[var(--app-text-muted)]">{formatDuration(execution.duration_ms)}</span>
                              </div>
                              <div className="mt-2 text-sm font-medium text-[var(--app-text)]">
                                {formatDateTime(execution.started_at)}
                              </div>
                              <p className="mt-1 line-clamp-2 text-xs text-[var(--app-text-muted)]">
                                {getExecutionPreview(execution)}
                              </p>
                            </button>
                          );
                        })}
                      </div>
                    </ScrollArea>
                  </aside>

                  <div className="flex min-h-[18rem] flex-col bg-[var(--app-surface)]">
                    {selectedExecution ? (
                      <>
                        <div className="border-b border-[var(--app-border)] bg-[var(--app-surface)] px-5 py-4">
                          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                            <div className="flex flex-wrap items-center gap-2">
                              {selectedExecutionBadge ? (
                                <Badge variant={selectedExecutionBadge.variant}>{selectedExecutionBadge.label}</Badge>
                              ) : null}
                              <span className="text-sm text-[var(--app-text-muted)]">
                                {formatDateTime(selectedExecution.started_at)}
                              </span>
                            </div>

                            <div className="flex flex-wrap items-center gap-2">
                              {selectedExecution.output ? (
                                <Button
                                  type="button"
                                  variant="outline"
                                  size="sm"
                                  className="border-[var(--app-border)] bg-[var(--app-surface-muted)] text-[var(--app-text)] hover:bg-[var(--app-bg-2)]"
                                  onClick={() => void handleCopyExecutionOutput(selectedExecution)}
                                >
                                  <Copy className="h-4 w-4" />
                                  Copy output
                                </Button>
                              ) : null}
                              <span className="text-sm text-[var(--app-text-muted)]">
                                Duration {formatDuration(selectedExecution.duration_ms)}
                              </span>
                            </div>
                          </div>

                          <div className="mt-4 grid gap-3 text-sm sm:grid-cols-2">
                            <div className="rounded-md border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-3 py-3">
                              <div className="text-xs text-[var(--app-text-muted)]">Started</div>
                              <div className="mt-1 font-medium text-[var(--app-text)]">
                                {formatDateTime(selectedExecution.started_at)}
                              </div>
                            </div>
                            <div className="rounded-md border border-[var(--app-border)] bg-[var(--app-surface-muted)] px-3 py-3">
                              <div className="text-xs text-[var(--app-text-muted)]">Finished</div>
                              <div className="mt-1 font-medium text-[var(--app-text)]">
                                {formatDateTime(selectedExecution.finished_at)}
                              </div>
                            </div>
                          </div>

                          {selectedExecution.error ? (
                            <p className="mt-4 rounded-md border border-destructive/20 bg-destructive/5 px-3 py-3 text-sm text-destructive">
                              {selectedExecution.error}
                            </p>
                          ) : null}
                        </div>

                        <div className="flex min-h-0 flex-1 flex-col bg-[var(--app-surface)] px-5 py-4">
                          <div className="mb-3 flex items-center justify-between gap-3">
                            <h3 className="text-sm font-medium text-[var(--app-text)]">Output</h3>
                            <span className="text-xs text-[var(--app-text-muted)]">
                              {selectedExecution.output
                                ? `${selectedExecutionOutputLineCount} ${selectedExecutionOutputLineCount === 1 ? "line" : "lines"}`
                                : "No output"}
                            </span>
                          </div>

                          {selectedExecution.output ? (
                            <ScrollArea className="min-h-0 flex-1 rounded-md border border-[var(--app-border)] bg-[var(--app-surface-muted)]">
                              <pre className="p-4 font-mono text-xs leading-5 whitespace-pre-wrap break-words text-[var(--app-text)]">
                                {selectedExecution.output}
                              </pre>
                            </ScrollArea>
                          ) : (
                            <div className="flex flex-1 items-center justify-center rounded-md border border-dashed border-[var(--app-border)] bg-[var(--app-surface-muted)] px-4 text-sm text-[var(--app-text-muted)]">
                              No output captured.
                            </div>
                          )}
                        </div>
                      </>
                    ) : null}
                  </div>
                </div>
              )
            ) : null}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
