import { Check } from 'lucide-react'

import { cn } from '@/lib/utils'

interface CheckCardOption {
  value: string
  label: React.ReactNode
  helper?: React.ReactNode
  disabled?: boolean
  disabledReason?: string
}

interface CheckCardGroupProps {
  options: CheckCardOption[]
  value: string[]
  onChange: (value: string[]) => void
  columns?: 1 | 2 | 3
  emptyMessage?: string
}

export function CheckCardGroup({
  options,
  value,
  onChange,
  columns = 3,
  emptyMessage = '暂无选项',
}: CheckCardGroupProps) {
  const selected = new Set(value)

  function toggle(v: string) {
    if (selected.has(v)) {
      onChange(value.filter((x) => x !== v))
    } else {
      onChange([...value, v])
    }
  }

  if (options.length === 0) {
    return <p className="text-body-sm text-ink-500">{emptyMessage}</p>
  }

  return (
    <div
      className={cn(
        'grid gap-space-3',
        columns === 1 && 'grid-cols-1',
        columns === 2 && 'grid-cols-1 md:grid-cols-2',
        columns === 3 && 'grid-cols-1 md:grid-cols-2 lg:grid-cols-3',
      )}
    >
      {options.map((option) => {
        const isSelected = selected.has(option.value)
        return (
          <label
            key={option.value}
            title={option.disabled ? option.disabledReason : undefined}
            className={cn(
              'relative flex cursor-pointer items-start gap-space-3 rounded-lg border p-space-4 transition-colors',
              isSelected
                ? 'border-primary bg-blueprint-tint text-blueprint'
                : 'border-border bg-surface text-ink-900 hover:border-border-strong',
              option.disabled && 'cursor-not-allowed opacity-50',
            )}
          >
            <input
              type="checkbox"
              className="sr-only"
              value={option.value}
              checked={isSelected}
              disabled={option.disabled}
              onChange={() => toggle(option.value)}
            />
            <span
              className={cn(
                'mt-0.5 flex size-4 shrink-0 items-center justify-center rounded border transition-colors',
                isSelected ? 'border-primary bg-primary text-white' : 'border-border bg-surface',
              )}
            >
              {isSelected && <Check className="size-3" strokeWidth={3} />}
            </span>
            <span className="flex min-w-0 flex-col">
              <span className="text-body-sm font-medium">{option.label}</span>
              {option.helper && <span className="text-caption text-ink-500">{option.helper}</span>}
            </span>
          </label>
        )
      })}
    </div>
  )
}
