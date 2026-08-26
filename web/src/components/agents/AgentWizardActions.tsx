import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface AgentWizardActionsProps {
  current: number
  total: number
  saving?: boolean
  onPrev: () => void
  onNext: () => void
  onSave: () => void
}

export function AgentWizardActions({
  current,
  total,
  saving,
  onPrev,
  onNext,
  onSave,
}: AgentWizardActionsProps) {
  const isLast = current === total - 1

  return (
    <div className="flex items-center justify-between border-t border-border pt-space-6">
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={current === 0 || saving}
        onClick={onPrev}
      >
        上一步
      </Button>

      {isLast ? (
        <Button
          type="button"
          size="sm"
          disabled={saving}
          onClick={onSave}
          className="bg-gradient-cta text-white hover:opacity-90"
        >
          {saving ? '保存中…' : '保存'}
        </Button>
      ) : (
        <Button
          type="button"
          size="sm"
          disabled={saving}
          onClick={onNext}
          className={cn('bg-primary text-primary-foreground hover:bg-primary/90')}
        >
          下一步
        </Button>
      )}
    </div>
  )
}
