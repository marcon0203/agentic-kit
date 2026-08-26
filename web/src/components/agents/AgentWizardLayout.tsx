interface AgentWizardLayoutProps {
  stepper: React.ReactNode
  card: React.ReactNode
  testPanel: React.ReactNode
}

export function AgentWizardLayout({ stepper, card, testPanel }: AgentWizardLayoutProps) {
  return (
    <div className="flex min-h-0 flex-1">
      <aside className="hidden w-[160px] shrink-0 overflow-y-auto border-r border-border bg-surface py-space-6 pl-space-6 pr-space-4 lg:block">
        {stepper}
      </aside>

      <main className="flex min-w-0 flex-1 justify-center overflow-y-auto bg-surface-page px-space-4 py-space-6">
        <div className="w-full max-w-[760px]">
          {card}
        </div>
      </main>

      {testPanel}
    </div>
  )
}
