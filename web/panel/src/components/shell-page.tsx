import { PageHeader } from "@/components/page-header";

type ShellPageProps = {
  title: string;
  meta: string;
  message: string;
};

export function ShellPage({ title, meta, message }: ShellPageProps) {
  return (
    <>
      <PageHeader title={title} meta={meta} />
      <div className="px-4 py-6 sm:px-6 lg:px-8">
        <section className="max-w-3xl rounded-2xl border border-[var(--app-border)] bg-[var(--app-surface)] p-6 shadow-[var(--app-shadow)]">
          <p className="text-[14px] leading-7 text-[var(--app-text-muted)]">{message}</p>
        </section>
      </div>
    </>
  );
}
