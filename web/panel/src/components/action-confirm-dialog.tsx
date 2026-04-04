import type { JSX, ReactNode } from 'react'
import { ConfirmDialog } from '@/components/confirm-dialog'

type ActionConfirmDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  desc: JSX.Element | string
  handleConfirm: () => void
  confirmText?: ReactNode
  cancelBtnText?: string
  destructive?: boolean
  isLoading?: boolean
  disabled?: boolean
  className?: string
  children?: ReactNode
}

export function ActionConfirmDialog(props: ActionConfirmDialogProps) {
  return <ConfirmDialog {...props} />
}
