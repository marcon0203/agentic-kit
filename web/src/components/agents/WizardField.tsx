interface FieldProps {
  label: string
  htmlFor: string
  helper?: string
  error?: string
  children: React.ReactNode
}

export function Field({ label, htmlFor, helper, error, children }: FieldProps) {
  return (
    <div className="flex flex-col gap-space-2">
      <label htmlFor={htmlFor} className="text-label-md text-ink-700">
        {label}
      </label>
      {children}
      {error ? <p className="text-caption text-rust">{error}</p> : helper ? <p className="text-caption text-ink-500">{helper}</p> : null}
    </div>
  )
}
