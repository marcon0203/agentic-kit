import { cn } from '@/lib/utils'

export interface WizardStep {
  id: string
  label: string
}

interface AgentWizardStepperProps {
  steps: WizardStep[]
  current: number
  completed: number
  onChange: (step: number) => void
}

export function AgentWizardStepper({ steps, current, completed, onChange }: AgentWizardStepperProps) {
  return (
    <nav aria-label="步骤" className="relative flex flex-col gap-space-2 pl-space-6">
      <div className="absolute top-2 bottom-2 left-[9px] w-[2px] bg-border" />
      {steps.map((step, index) => {
        const isActive = index === current
        const isCompleted = index <= completed && index !== current
        const reachable = index <= Math.max(current, completed + 1)
        return (
          <button
            key={step.id}
            type="button"
            disabled={!reachable}
            onClick={() => reachable && onChange(index)}
            className={cn(
              'relative z-10 flex items-center py-space-2 text-left text-body-sm transition-colors',
              isActive && 'font-medium text-ink-900',
              isCompleted && 'text-blueprint',
              !isActive && !isCompleted && (reachable ? 'text-ink-700 hover:text-ink-900' : 'text-ink-500'),
              !reachable && 'cursor-not-allowed',
            )}
          >
            <span
              className={cn(
                'absolute left-[-22px] top-1/2 size-[10px] -translate-y-1/2 rounded-full border-2',
                isActive && 'border-primary bg-primary',
                isCompleted && 'border-primary bg-primary',
                !isActive && !isCompleted && 'border-border bg-surface',
              )}
            />
            {step.label}
          </button>
        )
      })}
    </nav>
  )
}
