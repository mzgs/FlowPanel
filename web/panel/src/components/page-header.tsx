import type { ReactNode } from "react";

type PageHeaderProps = {
  title: string;
  meta?: string;
  actions?: ReactNode;
};

export function PageHeader({ title, meta, actions }: PageHeaderProps) {
  return (
    <div className="flex flex-col gap-3 border-b border-[var(--app-border)] px-5 py-4 md:flex-row md:items-center md:justify-between md:px-8">
      <div className="space-y-1">
        <h1 className="text-[22px] font-semibold tracking-[-0.02em] text-[var(--app-text)]">
          {title}
        </h1>
        {meta ? (
          <p className="text-[13px] text-[var(--app-text-muted)]">{meta}</p>
        ) : null}
      </div>
      {actions ? <div className="flex items-center gap-2">{actions}</div> : null}
    </div>
  );
}
