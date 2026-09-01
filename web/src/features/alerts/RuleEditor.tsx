import { useState, type FormEvent } from 'react'

import { useUpdateAlertRule, type AlertRule, type AlertRuleUpdateResponse } from '@/api/alerts'

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

function validate(values: FormValues): FormErrors {
  const errors: FormErrors = {}
  const threshold = Number.parseFloat(values.threshold)
  const forSeconds = Number.parseFloat(values.forSeconds)

  if (values.threshold.trim() === '' || !Number.isFinite(threshold)) {
    errors.threshold = 'Threshold must be a finite number.'
  }
  if (
    values.forSeconds.trim() === '' ||
    !Number.isInteger(forSeconds) ||
    forSeconds < 0 ||
    forSeconds > 2_147_483_647
  ) {
    errors.forSeconds = 'Duration must be an integer from 0 to 2147483647 seconds.'
  }
  return errors
}

function confirmation(response: AlertRuleUpdateResponse): string {
  return `Updated ${response.rule_id}: threshold ${response.threshold}, duration ${response.for_seconds}s, severity ${response.severity}.`
}

export function RuleEditor({ onCancel, rule }: RuleEditorProps) {
  const mutation = useUpdateAlertRule()
  const [values, setValues] = useState<FormValues>(() => initialValues(rule))
  const [errors, setErrors] = useState<FormErrors>({})
  const [saved, setSaved] = useState<string>()

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const nextErrors = validate(values)
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
          setSaved(confirmation(response))
        },
      },
    )
  }

  return (
    <section aria-labelledby="alert-rule-editor-title" className="space-y-4">
      <div>
        <h2 id="alert-rule-editor-title" className="text-lg font-semibold">
          Edit alert rule: {rule.id}
        </h2>
        <p className="text-muted-foreground text-sm">
          Changes are validated by the server and applied only after a successful save.
        </p>
      </div>

      <form className="grid max-w-xl gap-4" noValidate onSubmit={submit}>
        <label className="space-y-1 text-sm" htmlFor="rule-threshold">
          <span className="font-medium">Threshold</span>
          <input
            aria-describedby={errors.threshold ? 'rule-threshold-error' : undefined}
            className="border-input bg-background h-9 w-full rounded-md border px-3"
            id="rule-threshold"
            inputMode="decimal"
            onChange={(event) =>
              setValues((current) => ({ ...current, threshold: event.target.value }))
            }
            type="number"
            value={values.threshold}
          />
          {errors.threshold ? (
            <span id="rule-threshold-error" role="alert">
              {errors.threshold}
            </span>
          ) : null}
        </label>

        <label className="space-y-1 text-sm" htmlFor="rule-for-seconds">
          <span className="font-medium">Duration (seconds)</span>
          <input
            aria-describedby={errors.forSeconds ? 'rule-for-seconds-error' : undefined}
            className="border-input bg-background h-9 w-full rounded-md border px-3"
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
            <span id="rule-for-seconds-error" role="alert">
              {errors.forSeconds}
            </span>
          ) : null}
        </label>

        <label className="space-y-1 text-sm" htmlFor="rule-severity">
          <span className="font-medium">Severity</span>
          <select
            className="border-input bg-background h-9 w-full rounded-md border px-3"
            id="rule-severity"
            onChange={(event) =>
              setValues((current) => ({ ...current, severity: event.target.value as Severity }))
            }
            value={values.severity}
          >
            {SEVERITIES.map((severity) => (
              <option key={severity} value={severity}>
                {severity}
              </option>
            ))}
          </select>
        </label>

        <label className="flex items-center gap-2 text-sm" htmlFor="rule-enabled">
          <input
            checked={values.enabled}
            id="rule-enabled"
            onChange={(event) =>
              setValues((current) => ({ ...current, enabled: event.target.checked }))
            }
            type="checkbox"
          />
          <span className="font-medium">Enabled</span>
        </label>

        {mutation.isError ? <p role="alert">{mutation.error.message}</p> : null}
        {saved ? <p role="status">{saved}</p> : null}

        <div className="flex gap-2">
          <button
            className="rounded-md border px-3 py-2 text-sm"
            disabled={mutation.isPending}
            type="submit"
          >
            {mutation.isPending ? 'Saving…' : 'Save rule'}
          </button>
          <button className="rounded-md border px-3 py-2 text-sm" onClick={onCancel} type="button">
            Cancel
          </button>
        </div>
      </form>
    </section>
  )
}
