import * as React from 'react'
import { Eye, EyeOff, RefreshCw } from '@/components/icons/tabler-icons'
import { cn } from '@/lib/utils'
import { Button } from './ui/button'

type PasswordInputProps = Omit<
  React.InputHTMLAttributes<HTMLInputElement>,
  'type'
> & {
  ref?: React.Ref<HTMLInputElement>
  defaultVisible?: boolean
  onGeneratePassword?: () => void
}

export function PasswordInput({
  className,
  disabled,
  defaultVisible = false,
  onGeneratePassword,
  ref,
  ...props
}: PasswordInputProps) {
  const [showPassword, setShowPassword] = React.useState(defaultVisible)

  return (
    <div className={cn('relative rounded-md', className)}>
      <input
        type={showPassword ? 'text' : 'password'}
        className={cn(
          'flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs transition-colors file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-hidden disabled:cursor-not-allowed disabled:opacity-50',
          onGeneratePassword ? 'pe-14' : 'pe-8',
        )}
        ref={ref}
        disabled={disabled}
        {...props}
      />
      <div className='absolute end-1 top-1/2 flex -translate-y-1/2 items-center gap-1'>
        {onGeneratePassword ? (
          <Button
            type='button'
            size='icon'
            variant='ghost'
            disabled={disabled}
            className='h-6 w-6 rounded-md text-muted-foreground'
            onClick={onGeneratePassword}
            aria-label='Generate password'
            title='Generate password'
          >
            <RefreshCw size={16} />
          </Button>
        ) : null}
        <Button
          type='button'
          size='icon'
          variant='ghost'
          disabled={disabled}
          className='h-6 w-6 rounded-md text-muted-foreground'
          onClick={() => setShowPassword((prev) => !prev)}
          aria-label={showPassword ? 'Hide password' : 'Show password'}
          title={showPassword ? 'Hide password' : 'Show password'}
        >
          {showPassword ? <Eye size={18} /> : <EyeOff size={18} />}
        </Button>
      </div>
    </div>
  )
}
