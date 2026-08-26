import { cn } from '@/lib/utils'

interface TemperatureSliderProps {
  value: string
  onChange: (value: string) => void
  min?: number
  max?: number
  step?: number
}

export function TemperatureSlider({
  value,
  onChange,
  min = 0,
  max = 2,
  step = 0.1,
}: TemperatureSliderProps) {
  const num = value === '' ? 0.7 : Number(value)
  const display = Number.isNaN(num) ? '0.7' : num.toFixed(1)

  return (
    <div className="flex items-center gap-space-4">
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={num}
        onChange={(e) => onChange(e.target.value)}
        className={cn(
          'h-2 flex-1 cursor-pointer appearance-none rounded-full bg-border accent-primary',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50',
        )}
      />
      <span className="w-12 shrink-0 text-right font-mono text-body-md text-blueprint">
        {display}
      </span>
    </div>
  )
}
