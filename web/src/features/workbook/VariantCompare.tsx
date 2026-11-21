import { useMemo } from 'react'

import type { WorkbookVariant } from '@/api/workbooks'
import { Button } from '@/components/ui/button'
import { PlanComparison, type ComparedPlan } from '@/features/queries/PlanHistory'
import { useT, type Translate } from '@/i18n/react'
import { parsePlan } from '@/lib/planTree'

function compared(variant: WorkbookVariant, t: Translate): ComparedPlan {
  const hash = variant.plan_hash ?? ''
  return {
    label: t('workbook.compareLabel', { name: variant.name, hash: hash.slice(0, 12) }),
    parsed: parsePlan(variant.plan),
    hash,
  }
}

/** Two variants that ran, compared with the plan comparison of Plan History. */
export function VariantCompare({
  first,
  second,
  onClose,
}: {
  first: WorkbookVariant
  second: WorkbookVariant
  onClose: () => void
}) {
  const t = useT()
  const older = useMemo(() => compared(first, t), [first, t])
  const newer = useMemo(() => compared(second, t), [second, t])
  return (
    <section aria-label={t('workbook.compareTitle')} className="space-y-3">
      <div className="flex items-center justify-between gap-2">
        <h3 className="font-medium">{t('workbook.compareTitle')}</h3>
        <Button size="sm" variant="outline" onClick={onClose}>
          {t('workbook.compareClose')}
        </Button>
      </div>
      <PlanComparison
        newer={newer}
        newerHeading={t('workbook.compareSecond')}
        older={older}
        olderHeading={t('workbook.compareFirst')}
      />
    </section>
  )
}

export default VariantCompare
