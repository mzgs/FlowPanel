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
      <div className="px-5 py-5 md:px-8">
        <section className="max-w-3xl rounded-[10px] border border-[var(--app-border)] bg-[var(--app-surface)] p-5">
          <p className="text-[14px] leading-6 text-[var(--app-text-muted)]">{message}</p>
        </section>
      </div>
    </>
  );
}
