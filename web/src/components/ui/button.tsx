import * as React from 'react'
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'

import { cn } from '@/lib/utils'

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-sm text-body-sm font-semibold transition-all duration-150 ease-out disabled:pointer-events-none disabled:opacity-50 disabled:shadow-none [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4 outline-none focus-visible:ring-ring/50 focus-visible:ring-[3px] aria-invalid:ring-destructive/20 aria-invalid:border-destructive",
  {
    variants: {
      variant: {
        /* button-primary — the one and only accent button in a group. Flat
         * brand-color fill + a soft violet glow (shadow-accent); the full
         * gradient is reserved for hero-scale CTAs via className overrides
         * (e.g. the auth submit, subscribe buttons). */
        default:
          'bg-primary text-white shadow-[0_6px_16px_rgb(124_92_252_/_0.3)] hover:bg-violet',
        destructive: 'bg-destructive text-white shadow-xs hover:bg-destructive/90 focus-visible:ring-destructive/20',
        outline:
          'border border-border bg-surface text-ink-900 shadow-xs hover:border-border-strong hover:bg-surface-muted',
        secondary: 'bg-surface text-ink-700 border border-border hover:border-border-strong hover:bg-surface-muted',
        ghost: 'text-ink-700 hover:bg-blueprint-tint hover:text-blueprint',
        link: 'text-blueprint underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-9 px-4 has-[>svg]:px-3.5', // 36px — everyday actions
        sm: 'h-8 gap-1 px-3 text-caption has-[>svg]:px-2.5', // 32px — row/inline actions
        lg: 'h-11 px-6 text-body-md has-[>svg]:px-5', // 44px — the rare page-level hero CTA
        icon: 'size-9',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  },
)

function Button({
  className,
  variant,
  size,
  asChild = false,
  ...props
}: React.ComponentProps<'button'> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean
  }) {
  const Comp = asChild ? Slot : 'button'

  return (
    <Comp
      data-slot="button"
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  )
}

export { Button, buttonVariants }
