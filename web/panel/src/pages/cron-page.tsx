import { ShellPage } from "@/components/shell-page";

export function CronPage() {
  return (
    <ShellPage
      title="Cron"
      meta="No cron schedules are exposed in the current Step 1 baseline."
      message="The scheduler exists in the backend skeleton, but cron registration, execution history, and controls are still deferred."
    />
  );
}
