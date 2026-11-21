import { useState, type FormEvent } from 'react'

import { useUpdateAlertRule, type AlertRule, type AlertRuleUpdateResponse } from '@/api/alerts'
import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { Input } from '@/components/ui/input'
import { Callout } from '@/components/ui/callout'
import { useT, type Translate } from '@/i18n/react'
import { severityText } from './AlertRow'

const SEVERITIES = ['critical', 'warning', 'info'] as const
type Severity = (typeof SEVERITIES)[number]

interface RuleEditorProps {
  onCancel: () => void
  rule: AlertRule
}

interface FormValues {
  enabled: boolean
  forSeconds: string
  severity: Severity
  threshold: string
}

interface FormErrors {
  forSeconds?: string
  threshold?: string
}

function severityValue(value: string): Severity {
  return SEVERITIES.includes(value as Severity) ? (value as Severity) : 'warning'
}

function initialValues(rule: AlertRule): FormValues {
  return {
    enabled: rule.enabled ?? true,
    forSeconds: rule.for_seconds === undefined ? '' : String(rule.for_seconds),
    severity: severityValue(rule.severity),
    threshold: rule.threshold === undefined ? '' : String(rule.threshold),
  }
}

function validate(values: FormValues, t: Translate): FormErrors {
  const errors: FormErrors = {}
  const threshold = Number.parseFloat(values.threshold)
  const forSeconds = Number.parseFloat(values.forSeconds)

  if (values.threshold.trim() === '' || !Number.isFinite(threshold)) {
    errors.threshold = t('alerts.ruleEditor.thresholdInvalid')
  }
  if (
    values.forSeconds.trim() === '' ||
    !Number.isInteger(forSeconds) ||
    forSeconds < 0 ||
    forSeconds > 2_147_483_647
  ) {
    errors.forSeconds = t('alerts.ruleEditor.durationInvalid')
  }
  return errors
}

function confirmation(response: AlertRuleUpdateResponse, t: Translate): string {
  return t('alerts.ruleEditor.updated', {
    rule: response.rule_id,
    threshold: String(response.threshold),
    seconds: String(response.for_seconds),
    severity: severityText(response.severity, t),
  })
}

export function RuleEditor({ onCancel, rule }: RuleEditorProps) {
  const t = useT()
  const mutation = useUpdateAlertRule()
  const [values, setValues] = useState<FormValues>(() => initialValues(rule))
  const [errors, setErrors] = useState<FormErrors>({})
  const [saved, setSaved] = useState<string>()

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const nextErrors = validate(values, t)
    setErrors(nextErrors)
    setSaved(undefined)
    if (Object.keys(nextErrors).length > 0) {
      mutation.reset()
      return
    }

    mutation.reset()
    mutation.mutate(
      {
        ruleId: rule.id,
        update: {
          enabled: values.enabled,
          for_seconds: Number.parseFloat(values.forSeconds),
          severity: values.severity,
          threshold: Number.parseFloat(values.threshold),
        },
      },
      {
        onSuccess: (response) => {
          setValues({
            enabled: response.enabled,
            forSeconds: String(response.for_seconds),
            severity: severityValue(response.severity),
            threshold: String(response.threshold),
          })
          setSaved(confirmation(response, t))
        },
      },
    )
  }

  return (
    <section aria-labelledby="alert-rule-editor-title" className="space-y-4">
      <div>
        <h2 id="alert-rule-editor-title" className="text-lg font-semibold" tabIndex={-1}>
          {t('alerts.ruleEditor.title', { rule: rule.id })}
        </h2>
        <p className="text-muted-foreground text-sm">{t('alerts.ruleEditor.intro')}</p>
      </div>

      <form className="grid max-w-xl gap-4" noValidate onSubmit={submit}>
        <div className="space-y-1">
          <label className="space-y-1 text-sm" htmlFor="rule-threshold">
            <span className="font-medium">{t('alerts.rules.threshold')}</span>
            <Input
              aria-describedby={
                errors.threshold
                  ? 'rule-threshold-help rule-threshold-error'
                  : 'rule-threshold-help'
              }

              id="rule-threshold"
              inputMode="decimal"
              onChange={(event) =>
                setValues((current) => ({ ...current, threshold: event.target.value }))
              }
              type="number"
              value={values.threshold}
            />
            {errors.threshold ? (
              <span
                className="text-destructive block text-xs"
                id="rule-threshold-error"
                role="alert"
              >
                {errors.threshold}
              </span>
            ) : null}
          </label>
          <p className="text-muted-foreground text-xs" id="rule-threshold-help">
            {t('alerts.ruleEditor.thresholdHelp')}
          </p>
        </div>

        <div className="space-y-1">
          <label className="space-y-1 text-sm" htmlFor="rule-for-seconds">
            <span className="font-medium">{t('alerts.ruleEditor.duration')}</span>
            <Input
              aria-describedby={
                errors.forSeconds
                  ? 'rule-for-seconds-help rule-for-seconds-error'
                  : 'rule-for-seconds-help'
              }

              id="rule-for-seconds"
              min="0"
              onChange={(event) =>
                setValues((current) => ({ ...current, forSeconds: event.target.value }))
              }
              step="1"
              type="number"
              value={values.forSeconds}
            />
            {errors.forSeconds ? (
              <span
                className="text-destructive block text-xs"
                id="rule-for-seconds-error"
                role="alert"
              >
                {errors.forSeconds}
              </span>
            ) : null}
          </label>
          <p className="text-muted-foreground text-xs" id="rule-for-seconds-help">
            {t('alerts.ruleEditor.durationHelp')}
          </p>
        </div>

        <div className="space-y-1">
          <label className="space-y-1 text-sm" htmlFor="rule-severity">
            <span className="font-medium">{t('alerts.column.severity')}</span>
            <NativeSelect
              aria-describedby="rule-severity-help"
              className="w-full"
              id="rule-severity"
              onChange={(event) =>
                setValues((current) => ({ ...current, severity: event.target.value as Severity }))
              }
              value={values.severity}
            >
              {SEVERITIES.map((severity) => (
                <option key={severity} value={severity}>
                  {severityText(severity, t)}
                </option>
              ))}
            </NativeSelect>
          </label>
          <p className="text-muted-foreground text-xs" id="rule-severity-help">
            {t('alerts.ruleEditor.severityHelp')}
          </p>
        </div>

        <div className="space-y-1">
          <label className="flex items-center gap-2 text-sm" htmlFor="rule-enabled">
            <input
              aria-describedby="rule-enabled-help"
              checked={values.enabled}
              id="rule-enabled"
              onChange={(event) =>
                setValues((current) => ({ ...current, enabled: event.target.checked }))
              }
              type="checkbox"
            />
            <span className="font-medium">{t('alerts.rules.enabled')}</span>
          </label>
          <p className="text-muted-foreground text-xs" id="rule-enabled-help">
            {t('alerts.ruleEditor.enabledHelp')}
          </p>
        </div>

        {mutation.isError ? (
          <Callout variant="destructive">{mutation.error.message}</Callout>
        ) : null}
        {saved ? <p role="status">{saved}</p> : null}

        <div className="flex gap-2">
          <Button
            size="sm"

            disabled={mutation.isPending}
            type="submit"
          >
            {mutation.isPending ? t('alerts.ruleEditor.saving') : t('alerts.ruleEditor.save')}
          </Button>
          <Button size="sm" variant="outline" onClick={onCancel} type="button">
            {t('alerts.ruleEditor.cancel')}
          </Button>
        </div>
      </form>
    </section>
  )
}
