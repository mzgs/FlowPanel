import type { ReactNode } from "react";

type PageHeaderProps = {
  title: string;
  meta?: string;
  actions?: ReactNode;
};

export function PageHeader({ title, meta, actions }: PageHeaderProps) {
  return (
    <div className="border-b border-[var(--app-border)] bg-[var(--app-surface)] px-4 py-5 shadow-[var(--app-shadow)] sm:px-6 lg:px-8">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div className="space-y-1">
          <div className="text-[12px] font-semibold uppercase tracking-[0.18em] text-[var(--app-accent)]">
            FlowPanel
          </div>
          <h1 className="text-[26px] font-semibold tracking-[-0.03em] text-[var(--app-text)]">
            {title}
          </h1>
          {meta ? (
            <p className="max-w-3xl text-[14px] text-[var(--app-text-muted)]">{meta}</p>
          ) : null}
        </div>
        {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
      </div>
    </div>
  );
}
