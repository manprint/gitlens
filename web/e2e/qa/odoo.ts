import { assertStimulusAllowed, sqlInt } from './guard'
import { registerCleanup } from './pg'

/** Application stimulus over Odoo JSON-RPC (Odoo's own sessions reach PostgreSQL). */
const url = () => (process.env.ODOO_URL ?? 'http://127.0.0.1:9091').replace(/\/$/, '')
const db = () => process.env.ODOO_DB ?? 'odoo'
const user = () => process.env.ODOO_USER ?? 'admin'
const password = () => process.env.ODOO_PASSWORD ?? 'admin'

let nextId = 1

async function rpc<T>(service: string, method: string, args: unknown[]): Promise<T> {
  const response = await fetch(`${url()}/jsonrpc`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      jsonrpc: '2.0',
      method: 'call',
      id: nextId++,
      params: { service, method, args },
    }),
    signal: AbortSignal.timeout(60_000),
  })
  const body = (await response.json()) as { result?: T; error?: { data?: { message?: string } } }
  if (body.error)
    throw new Error(`odoo ${service}.${method}: ${body.error.data?.message ?? 'error'}`)
  return body.result as T
}

export interface OdooSession {
  uid: number
  call: <T>(
    model: string,
    method: string,
    args: unknown[],
    kwargs?: Record<string, unknown>,
  ) => Promise<T>
}

export async function login(): Promise<OdooSession> {
  const uid = await rpc<number | false>('common', 'login', [db(), user(), password()])
  if (!uid) throw new Error(`odoo login failed for ${user()}`)
  return {
    uid,
    call: (model, method, args, kwargs = {}) =>
      rpc('object', 'execute_kw', [db(), uid, password(), model, method, args, kwargs]),
  }
}

async function qaCategory(session: OdooSession): Promise<number> {
  const ids = await session.call<number[]>(
    'res.partner.category',
    'search',
    [[['name', '=', 'QA']]],
    {
      limit: 1,
    },
  )
  return ids[0] ?? (await session.call<number>('res.partner.category', 'create', [{ name: 'QA' }]))
}

export async function qaPartnerIds(session: OdooSession): Promise<number[]> {
  const category = await qaCategory(session)
  return session.call<number[]>('res.partner', 'search', [[['category_id', 'in', [category]]]])
}

/** Creates `n` partners in category QA; the cleanup deletes every QA partner. */
export async function createPartners(session: OdooSession, n: number): Promise<number[]> {
  assertStimulusAllowed()
  const category = await qaCategory(session)
  const ids: number[] = []
  for (let index = 0; index < Number(sqlInt(n)); index += 1) {
    ids.push(
      await session.call<number>('res.partner', 'create', [
        { name: `qa-partner-${Date.now()}-${index}`, category_id: [[6, 0, [category]]] },
      ]),
    )
  }
  registerCleanup(async () => {
    const left = await qaPartnerIds(session)
    if (left.length > 0) await session.call('res.partner', 'unlink', [left])
  })
  return ids
}

/** Creates `n` draft sale orders for a QA partner; the cleanup deletes them. */
export async function writeOrders(session: OdooSession, n: number): Promise<number[]> {
  assertStimulusAllowed()
  const [partner] = await createPartners(session, 1)
  const ids: number[] = []
  for (let index = 0; index < Number(sqlInt(n)); index += 1) {
    ids.push(await session.call<number>('sale.order', 'create', [{ partner_id: partner }]))
  }
  registerCleanup(async () => {
    await session.call('sale.order', 'unlink', [ids])
  })
  return ids
}

export function searchRead<T = Record<string, unknown>>(
  session: OdooSession,
  model: string,
  domain: unknown[],
  fields: string[] = ['id'],
  limit = 40,
): Promise<T[]> {
  return session.call<T[]>(model, 'search_read', [domain], { fields, limit })
}

/** Read-only browsing load: about `rate` search_read calls per second for `seconds`. */
export async function browseLoop(
  session: OdooSession,
  seconds: number,
  rate: number,
): Promise<number> {
  assertStimulusAllowed()
  const deadline = Date.now() + Number(sqlInt(seconds)) * 1000
  const pause = 1000 / Number(sqlInt(rate))
  let calls = 0
  while (Date.now() < deadline) {
    const started = Date.now()
    await searchRead(session, 'res.partner', [], ['name', 'email'], 40)
    calls += 1
    await new Promise((resolve) => setTimeout(resolve, Math.max(0, pause - (Date.now() - started))))
  }
  return calls
}
