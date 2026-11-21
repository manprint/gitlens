import { useState, type FormEvent } from 'react'

import { Button } from '@/components/ui/button'
import { Callout } from '@/components/ui/callout'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { useT, type Translate } from '@/i18n/react'
import {
  HINT_CHARACTERS,
  WORKBOOK_SETTINGS,
  draftProblems,
  settingProblem,
  type Problem,
  type SettingRule,
  type VariantDraft,
} from '@/lib/workbook'

interface VariantEditorProps {
  initial: VariantDraft
  editing: boolean
  canAnalyze: boolean
  saving: boolean
  serverError?: string | undefined
  onSave: (draft: VariantDraft) => void
  onCancel: () => void
}

function problemText(problem: Problem, t: Translate): string {
  return t(problem.key, problem.values)
}

function defaultValue(rule: SettingRule): string {
  if (rule.kind === 'bool') return 'off'
  if (rule.options) return rule.options[0] ?? ''
  return String(rule.min ?? 0)
}

function SettingInput({
  rule,
  value,
  onChange,
}: {
  rule: SettingRule
  value: string
  onChange: (value: string) => void
}) {
  const t = useT()
  if (rule.kind === 'bool') {
    return (
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          aria-label={rule.name}
          checked={value === 'on'}
          onChange={(event) => onChange(event.target.checked ? 'on' : 'off')}
        />
        <span className="font-mono">{value}</span>
      </label>
    )
  }
  if (rule.options) {
    return (
      <NativeSelect aria-label={rule.name} value={value} onChange={(e) => onChange(e.target.value)}>
        {rule.options.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
      </NativeSelect>
    )
  }
  const invalid = settingProblem(rule.name, value) !== null
  return (
    <span className="flex items-center gap-2">
      <Input
        aria-label={rule.name}
        aria-invalid={invalid}
        className="w-40"
        inputMode={rule.kind === 'decimal' ? 'decimal' : 'numeric'}
        max={rule.max}
        min={rule.min}
        step={rule.kind === 'decimal' ? 'any' : 1}
        type="number"
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
      <span className="text-muted-foreground text-xs">
        {t('workbook.editor.range', {
          min: rule.min ?? 0,
          max: rule.max ?? 0,
          unit: rule.unit ?? '',
        })}
      </span>
    </span>
  )
}

/**
 * The form of one variant: settings from the allowed list with an input per
 * value rule, hints, parameter values and ANALYZE. It refuses what the server
 * would refuse before sending.
 */
export function VariantEditor({
  initial,
  editing,
  canAnalyze,
  saving,
  serverError,
  onSave,
  onCancel,
}: VariantEditorProps) {
  const t = useT()
  const [draft, setDraft] = useState<VariantDraft>(initial)
  const [problems, setProblems] = useState<Problem[]>([])
  const unused = WORKBOOK_SETTINGS.filter((rule) => !(rule.name in draft.settings))

  const update = (patch: Partial<VariantDraft>) => setDraft((current) => ({ ...current, ...patch }))
  const setSetting = (name: string, value: string | null) => {
    const settings = { ...draft.settings }
    if (value === null) delete settings[name]
    else settings[name] = value
    update({ settings })
  }
  const submit = (event: FormEvent) => {
    event.preventDefault()
    const found = draftProblems(draft)
    setProblems(found)
    if (found.length === 0) onSave(draft)
  }

  return (
    <form
      aria-label={editing ? t('workbook.editor.editTitle') : t('workbook.editor.newTitle')}
      className="bg-card space-y-4 rounded-xl border p-4"
      noValidate
      onSubmit={submit}
    >
      <h3 className="font-medium">
        {editing ? t('workbook.editor.editTitle') : t('workbook.editor.newTitle')}
      </h3>
      <label className="block space-y-1 text-sm">
        <span>{t('workbook.editor.name')}</span>
        <Input value={draft.name} onChange={(event) => update({ name: event.target.value })} />
      </label>

      <fieldset className="space-y-2">
        <legend className="text-sm font-medium">{t('workbook.editor.settings')}</legend>
        {Object.entries(draft.settings).map(([name, value]) => {
          const rule = WORKBOOK_SETTINGS.find((candidate) => candidate.name === name)
          if (!rule) return null
          return (
            <div key={name} className="flex flex-wrap items-center gap-3">
              <code className="w-64 text-sm">{name}</code>
              <SettingInput rule={rule} value={value} onChange={(next) => setSetting(name, next)} />
              <Button
                size="xs"
                type="button"
                variant="ghost"
                onClick={() => setSetting(name, null)}
              >
                {t('workbook.editor.removeSetting', { name })}
              </Button>
            </div>
          )
        })}
        {unused.length > 0 && (
          <NativeSelect
            aria-label={t('workbook.editor.chooseSetting')}
            value=""
            onChange={(event) => {
              const rule = WORKBOOK_SETTINGS.find((c) => c.name === event.target.value)
              if (rule) setSetting(rule.name, defaultValue(rule))
            }}
          >
            <option value="">{t('workbook.editor.chooseSetting')}</option>
            {unused.map((rule) => (
              <option key={rule.name} value={rule.name}>
                {rule.name}
              </option>
            ))}
          </NativeSelect>
        )}
      </fieldset>

      <label className="block space-y-1 text-sm">
        <span>{t('workbook.editor.hints')}</span>
        <Input
          className="font-mono"
          value={draft.hints}
          onChange={(event) => update({ hints: event.target.value })}
        />
        <span className="text-muted-foreground block text-xs">
          {t('workbook.editor.hintsRule', { allowed: HINT_CHARACTERS })}
        </span>
      </label>

      <fieldset className="space-y-2">
        <legend className="text-sm font-medium">{t('workbook.editor.params')}</legend>
        {draft.params.map((param, index) => (
          <div key={index} className="flex items-center gap-3">
            <Input
              aria-label={t('workbook.editor.paramLabel', { index: index + 1 })}
              className="w-64"
              disabled={param === null}
              value={param ?? ''}
              onChange={(event) => {
                const params = [...draft.params]
                params[index] = event.target.value
                update({ params })
              }}
            />
            <label className="flex items-center gap-1 text-xs">
              <input
                type="checkbox"
                checked={param === null}
                onChange={(event) => {
                  const params = [...draft.params]
                  params[index] = event.target.checked ? null : ''
                  update({ params })
                }}
              />
              {t('workbook.editor.paramNull', { index: index + 1 })}
            </label>
            <Button
              size="xs"
              type="button"
              variant="ghost"
              onClick={() => update({ params: draft.params.filter((_, i) => i !== index) })}
            >
              {t('workbook.editor.removeParam', { index: index + 1 })}
            </Button>
          </div>
        ))}
        <Button
          size="sm"
          type="button"
          variant="outline"
          onClick={() => update({ params: [...draft.params, ''] })}
        >
          {t('workbook.editor.addParam')}
        </Button>
      </fieldset>

      <div className="space-y-1">
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={draft.analyze}
            disabled={!canAnalyze && !draft.analyze}
            onChange={(event) => update({ analyze: event.target.checked })}
          />
          {t('workbook.editor.analyze')}
        </label>
        {draft.analyze && <Callout variant="warning">{t('queries.explain.analyzeText')}</Callout>}
      </div>

      {(problems.length > 0 || serverError) && (
        <Callout role="alert" variant="destructive">
          <ul className="list-disc pl-4">
            {problems.map((problem, index) => (
              <li key={index}>{problemText(problem, t)}</li>
            ))}
            {serverError && <li>{serverError}</li>}
          </ul>
        </Callout>
      )}

      <div className="flex gap-2">
        <Button disabled={saving} type="submit">
          {t('workbook.editor.save')}
        </Button>
        <Button type="button" variant="outline" onClick={onCancel}>
          {t('workbook.editor.cancel')}
        </Button>
      </div>
    </form>
  )
}

export default VariantEditor
