import type { ReactNode } from "react";

type PageHeaderProps = {
  title: ReactNode;
  meta?: string;
  actions?: ReactNode;
};

export function PageHeader({ title, meta, actions }: PageHeaderProps) {
  return (
    <div className="px-4 py-6 sm:px-6 lg:px-8">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold tracking-tight text-foreground">{title}</h1>
          {meta ? <p className="max-w-3xl text-sm text-muted-foreground">{meta}</p> : null}
        </div>
        {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
      </div>
    </div>
  );
}
