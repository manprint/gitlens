import { fireEvent, screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import {
  WorkbookRequestError,
  useCreateWorkbook,
  useDeleteVariant,
  useDeleteWorkbook,
  useRunVariants,
  useSaveVariant,
  useUpdateWorkbook,
  useWorkbook,
  useWorkbooks,
  type Workbook,
} from './workbooks'

const BOOK: Workbook = {
  workbook_id: 'w-1',
  instance_id: 'i-1',
  database: 'app',
  queryid: '42',
  name: 'Tuning 42',
  notes: '',
  created_at: '2026-11-21T10:00:00Z',
  updated_at: '2026-11-21T10:00:00Z',
  variants: [],
}

const BODY = { name: 'V', settings: {}, hints: '', params: [], analyze: false }

function Probe() {
  const list = useWorkbooks('i-1', '42')
  const one = useWorkbook('w-1')
  const create = useCreateWorkbook()
  const update = useUpdateWorkbook('w-1')
  const drop = useDeleteWorkbook('w-1')
  const save = useSaveVariant('w-1')
  const remove = useDeleteVariant('w-1')
  const run = useRunVariants('w-1')
  const failed = [list, one, create, update, drop, save, remove, run]
    .map((hook) => hook.error)
    .find((error) => error)
  return (
    <div>
      <output data-testid="list">{list.data?.length ?? '-'}</output>
      <output data-testid="one">{one.data?.name ?? '-'}</output>
      <output data-testid="run">{run.data?.join(',') ?? '-'}</output>
      <output data-testid="error">
        {failed ? `${failed.code ?? '-'}|${failed.message}` : '-'}
      </output>
      <button
        onClick={() =>
          create.mutate({ instance_id: 'i-1', database: 'app', queryid: '42', name: 'N' })
        }
      >
        create
      </button>
      <button onClick={() => update.mutate({ notes: 'n' })}>update</button>
      <button onClick={() => drop.mutate()}>drop</button>
      <button onClick={() => save.mutate({ body: BODY })}>add</button>
      <button onClick={() => save.mutate({ variantId: 'v-1', body: BODY })}>replace</button>
      <button onClick={() => remove.mutate('v-1')}>remove</button>
      <button onClick={() => run.mutate(['v-1', 'v-2'])}>run</button>
    </div>
  )
}

describe('workbook API client', () => {
  beforeEach(() => {
    vi.useRealTimers()
  })

  it('UI-WBK-010 every workbook call reaches its endpoint with its method', async () => {
    const calls: string[] = []
    const record =
      (label: string, body: unknown, status = 200) =>
      () => {
        calls.push(label)
        return HttpResponse.json(body as Record<string, unknown>, { status })
      }
    server.use(
      http.get('*/api/v1/workbooks', record('list', { workbooks: [BOOK] })),
      http.post('*/api/v1/workbooks', record('create', BOOK, 201)),
      http.get('*/api/v1/workbooks/:id', record('get', BOOK)),
      http.patch('*/api/v1/workbooks/:id', record('update', BOOK)),
      http.delete('*/api/v1/workbooks/:id', () => {
        calls.push('drop')
        return new HttpResponse(null, { status: 204 })
      }),
      http.post('*/api/v1/workbooks/:id/variants', record('add', { variant_id: 'v-9' }, 201)),
      http.put(
        '*/api/v1/workbooks/:id/variants/:variantId',
        record('replace', { variant_id: 'v-1' }),
      ),
      http.delete('*/api/v1/workbooks/:id/variants/:variantId', () => {
        calls.push('remove')
        return new HttpResponse(null, { status: 204 })
      }),
      http.post('*/api/v1/workbooks/:id/variants/:variantId/run', ({ params }) => {
        calls.push(`run:${String(params.variantId)}`)
        return HttpResponse.json({ command_id: `c-${String(params.variantId)}` }, { status: 202 })
      }),
    )
    renderWithProviders(<Probe />)
    await waitFor(() => expect(screen.getByTestId('list')).toHaveTextContent('1'))
    expect(screen.getByTestId('one')).toHaveTextContent('Tuning 42')
    for (const name of ['create', 'update', 'drop', 'add', 'replace', 'remove', 'run'])
      fireEvent.click(screen.getByRole('button', { name }))
    await waitFor(() => expect(screen.getByTestId('run')).toHaveTextContent('c-v-1,c-v-2'))
    await waitFor(() =>
      expect(calls).toEqual(
        expect.arrayContaining([
          'create',
          'update',
          'drop',
          'add',
          'replace',
          'remove',
          'run:v-1',
          'run:v-2',
        ]),
      ),
    )
    expect(screen.getByTestId('error')).toHaveTextContent('-')
  })

  it('UI-WBK-011 a refused request keeps the server error code', async () => {
    server.use(
      http.get('*/api/v1/workbooks', () => HttpResponse.json({ workbooks: [] })),
      http.get('*/api/v1/workbooks/:id', () => HttpResponse.json(BOOK)),
      http.post('*/api/v1/workbooks', () =>
        HttpResponse.json(
          { error: 'limit_reached', detail: 'at most 100 workbooks per server' },
          { status: 409 },
        ),
      ),
    )
    renderWithProviders(<Probe />)
    await waitFor(() => expect(screen.getByTestId('one')).toHaveTextContent('Tuning 42'))
    fireEvent.click(screen.getByRole('button', { name: 'create' }))
    await waitFor(() =>
      expect(screen.getByTestId('error')).toHaveTextContent(
        'limit_reached|limit_reached: at most 100 workbooks per server',
      ),
    )
  })

  it('UI-WBK-012 errors without an envelope still read as text', () => {
    const network = new WorkbookRequestError({ kind: 'network', message: 'offline' })
    expect(network.message).toBe('offline')
    expect(network.code).toBeUndefined()
    expect(new WorkbookRequestError({ kind: 'not_found' }).message).toBe('not_found')
  })
})
