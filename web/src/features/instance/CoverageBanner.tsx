import { useCoverage } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { InfoHint } from '@/components/layout/InfoHint'
import { Callout } from '@/components/ui/callout'

type Coverage = Schemas['CoverageResponse']

/**
 * Minutes until the last pending check is expected, counted from the
 * response's own `generated_at` so a skewed browser clock does not change it.
 * Null when no pending check has an expectation (an agent that does not
 * report its start); zero or less when the expectation has passed.
 */
function minutesUntilExpected(coverage: Coverage): number | null {
  const generated = Date.parse(coverage.generated_at)
  const expected = coverage.checks
    .filter((check) => check.state === 'pending' && check.expected_by !== null)
    .map((check) => Date.parse(check.expected_by!))
    .filter(Number.isFinite)
  if (expected.length === 0 || !Number.isFinite(generated)) return null
  return Math.ceil((Math.max(...expected) - generated) / 60_000)
}

/**
 * The parts of the banner's sentence: what the banner is about, how many
 * checks have a current result, and when the pending ones are due. A failed
 * check has no current result (its data is missing), and is named apart; it
 * is not due, so with one failing the due time speaks of the pending checks
 * only.
 */
function coverageParts(coverage: Coverage): { lead: string; count: string; due: string } {
  const pending = coverage.checks.filter((check) => check.state === 'pending').length
  const failed = coverage.checks.some((check) => check.state === 'failed')
  const count = `${coverage.reported} of ${coverage.total} checks have a current result`
  if (pending === 0) return { lead: 'Incomplete data', count, due: '' }
  const minutes = minutesUntilExpected(coverage)
  const rest = failed ? 'the pending ones' : 'the rest'
  const due =
    minutes === null
      ? ''
      : minutes > 0
        ? ` — ${rest} are expected within ${minutes} min`
        : ` — ${rest} are overdue`
  return { lead: 'Collecting data', count, due }
}

/**
 * How complete the picture of an instance is since its agent started (13.3,
 * D22d): shown while some checks have no current result, gone once all have.
 * A check whose last run failed keeps the banner, because its data is missing,
 * and is named. The text is a polite live region, so a screen reader hears the
 * count change without being interrupted.
 */
export function CoverageBanner({
  instanceId,
  compact = false,
}: {
  instanceId: string
  /**
   * One line for the header row: the count never truncates, the rest of the
   * sentence does, and the whole of it is in the ⓘ for every input.
   */
  compact?: boolean
}) {
  const coverage = useCoverage(instanceId)
  const data = coverage.data
  if (data === undefined || data.total === 0 || data.reported >= data.total) return null
  const failed = data.checks.filter((check) => check.state === 'failed').map((check) => check.check)
  const { lead, count, due } = coverageParts(data)
  const sentence = `${lead}: ${count}${due}.`
  const failedText =
    failed.length === 0
      ? null
      : `${failed.length === 1 ? 'Failed on its last run: ' : 'Failed on their last run: '}${failed.join(', ')}.`
  const common = {
    'aria-live': 'polite',
    'data-caption-id': 'instance.coverage',
    'data-testid': 'coverage-banner',
    role: 'status',
    variant: failed.length > 0 ? 'warning' : 'default',
  } as const
  if (compact) {
    const rest = `${lead}${due}.${failedText === null ? '' : ` ${failedText}`}`
    return (
      <Callout {...common} className="flex min-w-0 flex-1 items-center gap-1.5 px-2.5 py-1">
        <span className="shrink-0 font-medium whitespace-nowrap" data-testid="coverage-count">
          {data.reported}/{data.total} checks
        </span>
        <span
          className="text-muted-foreground block w-0 min-w-0 flex-1 truncate"
          data-testid="coverage-line"
          title={failedText === null ? sentence : `${sentence} ${failedText}`}
        >
          {rest}
        </span>
        <InfoHint
          className="shrink-0"
          extra={
            <>
              <p data-testid="coverage-hint-sentence">{sentence}</p>
              {failedText === null ? null : <p data-testid="coverage-hint-failed">{failedText}</p>}
            </>
          }
          id="instance.coverage"
          label="Data collection"
        />
      </Callout>
    )
  }
  return (
    <Callout {...common} className="flex items-start gap-2 py-2 min-[1600px]:py-1.5">
      <div className="min-w-0 flex-1 min-[1600px]:flex min-[1600px]:items-baseline min-[1600px]:gap-3">
        <p className="m-0">{sentence}</p>
        {failedText === null ? null : (
          <p className="text-muted-foreground m-0" data-testid="coverage-failed">
            {failedText}
          </p>
        )}
      </div>
      <InfoHint className="shrink-0" id="instance.coverage" label="Data collection" />
    </Callout>
  )
}
