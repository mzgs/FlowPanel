import { useEffect, useState, type FormEvent } from "react";
import {
  createCronJob,
  deleteCronJob,
  fetchCronJobs,
  type CronApiError,
  type CronJob,
} from "@/api/cron";
import { Clock, LoaderCircle, RefreshCw, TerminalSquare, Trash2 } from "@/components/icons/tabler-icons";
import { PageHeader } from "@/components/page-header";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
  return Array.isArray(jobs) ? jobs : [];
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
  const [deletingJobId, setDeletingJobId] = useState<string | null>(null);
  const schedulerBadge = getSchedulerBadge(enabled, started);

  useEffect(() => {
    let active = true;

    async function loadJobs() {
      try {
        const payload = await fetchCronJobs();
        if (!active) {
          return;
        }

        setJobs(normalizeJobs(payload.jobs));
        setEnabled(payload.enabled);
        setStarted(payload.started);
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

  async function handleRefresh() {
    setLoading(true);

    try {
      const payload = await fetchCronJobs();
      setJobs(normalizeJobs(payload.jobs));
      setEnabled(payload.enabled);
      setStarted(payload.started);
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
      const createdJob = await createCronJob({
        name: form.name.trim(),
        schedule: form.schedule.trim(),
        command: form.command.trim(),
      });

      setJobs((currentJobs) => [createdJob, ...currentJobs]);
      setForm(initialForm);
      setErrors({});
    } catch (error) {
      const apiError = error as CronApiError;
      setErrors(apiError.fieldErrors ?? {});
      setFormError(getErrorMessage(error, "Failed to create cron job."));
    } finally {
      setSubmitting(false);
    }
  }

  async function handleDelete(job: CronJob) {
    const confirmed = window.confirm(`Delete cron job "${job.name}"?`);
    if (!confirmed) {
      return;
    }

    setDeletingJobId(job.id);

    try {
      await deleteCronJob(job.id);
      setJobs((currentJobs) => currentJobs.filter((currentJob) => currentJob.id !== job.id));
    } catch (error) {
      setLoadError(getErrorMessage(error, "Failed to delete cron job."));
    } finally {
      setDeletingJobId(null);
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
              <h2 className="text-base font-semibold tracking-tight">New job</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                Save a schedule, label it clearly, and run any shell command available on this server.
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

              <Button type="submit" className="w-full" disabled={submitting}>
                {submitting ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
                Add job
              </Button>
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
                    <TableHead className="w-[96px] text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {jobs.map((job) => (
                    <TableRow key={job.id}>
                      <TableCell className="font-medium">{job.name}</TableCell>
                      <TableCell className="font-mono text-xs">{job.schedule}</TableCell>
                      <TableCell className="max-w-[28rem] whitespace-normal break-all font-mono text-xs text-muted-foreground">
                        {job.command}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">{formatDateTime(job.created_at)}</TableCell>
                      <TableCell className="text-right">
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          onClick={() => void handleDelete(job)}
                          disabled={deletingJobId === job.id}
                          aria-label={`Delete ${job.name}`}
                        >
                          {deletingJobId === job.id ? (
                            <LoaderCircle className="h-4 w-4 animate-spin" />
                          ) : (
                            <Trash2 className="h-4 w-4" />
                          )}
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </section>
        </div>
      </div>
    </>
  );
}
