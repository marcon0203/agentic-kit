import { Toaster as Sonner, type ToasterProps } from 'sonner'

// Replaces the deprecated shadcn `toast` component: the project's shadcn
// registry entry for `toast` now points here (sonner), per upstream shadcn
// guidance — kept as `toast` in task_implement.json's component list.
const Toaster = ({ ...props }: ToasterProps) => {
  return (
    <Sonner
      theme="light"
      className="toaster group"
      style={
        {
          '--normal-bg': 'var(--popover)',
          '--normal-text': 'var(--popover-foreground)',
          '--normal-border': 'var(--border)',
        } as React.CSSProperties
      }
      {...props}
    />
  )
}

export { Toaster }
