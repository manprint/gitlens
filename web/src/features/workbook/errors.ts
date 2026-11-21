import type { WorkbookRequestError, WorkbookVariant } from '@/api/workbooks'
import type { Translate } from '@/i18n/react'
import { isHintPlanProblem } from '@/lib/workbook'

/** The text a failed request shows; limit and pending get their own sentence. */
export function requestErrorText(error: WorkbookRequestError, t: Translate): string {
  if (error.code === 'limit_reached') return t('workbook.error.limit')
  if (error.code === 'variant_pending') return t('workbook.error.pending')
  if (isHintPlanProblem(error.message)) return t('workbook.hintPlanInactive')
  return error.message
}

/** The text of a failed variant: the hint_plan sentence for a missing pg_hint_plan. */
export function variantErrorText(variant: WorkbookVariant, t: Translate): string {
  return isHintPlanProblem(variant.error) ? t('workbook.hintPlanInactive') : (variant.error ?? '')
}
