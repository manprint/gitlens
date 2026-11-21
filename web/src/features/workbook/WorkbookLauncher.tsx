import { useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { useCreateWorkbook, useWorkbooks } from '@/api/workbooks'
import { Button } from '@/components/ui/button'
import { Callout } from '@/components/ui/callout'
import { Input } from '@/components/ui/input'
import { useT } from '@/i18n/react'
import { workbookPath } from '@/lib/workbook'

import { requestErrorText } from './errors'

/** The query detail entry: the statement's workbooks and a form for a new one. */
export function WorkbookLauncher({
  instanceId,
  queryid,
  datname,
}: {
  instanceId: string
  queryid: string
  datname?: string
}) {
  const t = useT()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const list = useWorkbooks(instanceId, queryid, open)
  const create = useCreateWorkbook()
  const database = datname ?? ''
  const books = (list.data ?? []).filter((book) => !datname || book.database === datname)

  const submit = (event: FormEvent) => {
    event.preventDefault()
    create.mutate(
      {
        instance_id: instanceId,
        queryid,
        database,
        name: name.trim() || t('workbook.defaultName', { queryid }),
      },
      { onSuccess: (book) => void navigate(workbookPath(instanceId, queryid, book.workbook_id)) },
    )
  }

  return (
    <section aria-label={t('workbook.listTitle')} className="space-y-3">
      <Button aria-expanded={open} variant="outline" onClick={() => setOpen((value) => !value)}>
        {t('workbook.button')}
      </Button>
      {open && (
        <div className="bg-card space-y-3 rounded-xl border p-4">
          <h3 className="font-medium">{t('workbook.listTitle')}</h3>
          {books.length === 0 ? (
            <p className="text-muted-foreground text-sm">{t('workbook.none')}</p>
          ) : (
            <ul className="space-y-1 text-sm">
              {books.map((book) => (
                <li key={book.workbook_id} className="flex items-center gap-2">
                  <Link
                    className="underline"
                    to={workbookPath(instanceId, queryid, book.workbook_id)}
                  >
                    {book.name}
                  </Link>
                  <span className="text-muted-foreground">
                    {t('workbook.variantCount', { count: book.variants.length })}
                  </span>
                </li>
              ))}
            </ul>
          )}
          <form className="flex flex-wrap items-center gap-2" onSubmit={submit}>
            <Input
              aria-label={t('workbook.newName')}
              className="w-72"
              placeholder={t('workbook.defaultName', { queryid })}
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
            <Button disabled={create.isPending} size="sm" type="submit">
              {t('workbook.new')}
            </Button>
          </form>
          {create.error && (
            <Callout role="alert" variant="destructive">
              {requestErrorText(create.error, t)}
            </Callout>
          )}
        </div>
      )}
    </section>
  )
}

export default WorkbookLauncher
