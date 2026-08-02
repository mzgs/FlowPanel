import * as React from 'react'
import { cn } from '@/lib/utils'

const tableActionButtonClassName =
  'inline-flex size-8 shrink-0 items-center justify-center rounded-md border border-transparent bg-transparent text-muted-foreground shadow-none outline-none transition-colors hover:border-border hover:bg-muted hover:text-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40 disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:size-[18px]'
const tableDangerActionButtonClassName =
  'inline-flex size-8 shrink-0 items-center justify-center rounded-md border border-transparent bg-transparent text-destructive shadow-none outline-none transition-colors hover:border-destructive/30 hover:bg-destructive/10 hover:text-destructive focus-visible:border-destructive/50 focus-visible:ring-[3px] focus-visible:ring-destructive/30 disabled:cursor-not-allowed disabled:opacity-50 [&_svg]:size-[18px]'
const tableActionGroupClassName = 'flex items-center justify-end gap-1'

function Table({ className, ...props }: React.ComponentProps<'table'>) {
  return (
    <div
      data-slot='table-container'
      className='relative w-full overflow-x-auto'
    >
      <table
        data-slot='table'
        className={cn('w-full caption-bottom text-sm', className)}
        {...props}
      />
    </div>
  )
}

function TableHeader({ className, ...props }: React.ComponentProps<'thead'>) {
  return (
    <thead
      data-slot='table-header'
      className={cn('bg-muted/70 text-muted-foreground [&_tr]:border-b', className)}
      {...props}
    />
  )
}

function TableBody({ className, ...props }: React.ComponentProps<'tbody'>) {
  return (
    <tbody
      data-slot='table-body'
      className={cn('[&_tr:last-child]:border-0', className)}
      {...props}
    />
  )
}

function TableRow({ className, ...props }: React.ComponentProps<'tr'>) {
  return (
    <tr
      data-slot='table-row'
      className={cn(
        'border-b border-border/80 transition-colors hover:bg-muted/65 data-[state=selected]:bg-muted',
        className
      )}
      {...props}
    />
  )
}

function TableHead({ className, ...props }: React.ComponentProps<'th'>) {
  return (
    <th
      data-slot='table-head'
      className={cn(
        'h-9 px-3 text-start align-middle font-semibold whitespace-nowrap text-foreground [&>[role=checkbox]]:translate-y-[2px]',
        className
      )}
      {...props}
    />
  )
}

function TableCell({ className, ...props }: React.ComponentProps<'td'>) {
  return (
    <td
      data-slot='table-cell'
      className={cn(
        'px-3 py-[5px] align-middle whitespace-nowrap [&>[role=checkbox]]:translate-y-[2px]',
        className
      )}
      {...props}
    />
  )
}

export {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
  tableActionButtonClassName,
  tableDangerActionButtonClassName,
  tableActionGroupClassName,
}
