import type { Schemas } from '@/api/types'

import { INSTANCE_ID } from '../fixture-helpers'

type SearchResponse = Schemas['SearchResponse']

export const responseStatus = 200

/**
 * A search for `orders`: two statements, an index found by its definition, the
 * table, and 63 columns of which the server returned the first 50 (D20, I-9).
 */
export const base: SearchResponse = {
  q: 'orders',
  groups: [
    {
      kind: 'statements',
      total: 2,
      truncated: false,
      hits: [
        {
          key: 'app.4242',
          title: '4242',
          subtitle: 'app',
          snippet: [
            { text: 'SELECT * FROM ', match: false },
            { text: 'orders', match: true },
            { text: ' WHERE customer_id = $1', match: false },
          ],
          link: `/queries?instance=${INSTANCE_ID}&q=orders`,
        },
        {
          key: 'app.-77',
          title: '-77',
          subtitle: 'app',
          snippet: [
            { text: 'UPDATE ', match: false },
            { text: 'Orders', match: true },
            { text: ' SET state = $1 WHERE id = $2', match: false },
          ],
          link: `/queries?instance=${INSTANCE_ID}&q=orders`,
        },
      ],
    },
    {
      kind: 'indexes',
      total: 1,
      truncated: false,
      hits: [
        {
          key: 'app.public.orders_customer_idx',
          title: 'public.orders_customer_idx',
          subtitle: 'app · public.orders',
          snippet: [
            { text: 'CREATE INDEX ', match: false },
            { text: 'orders', match: true },
            { text: '_customer_idx ON public.', match: false },
            { text: 'orders', match: true },
            { text: ' USING btree (customer_id)', match: false },
          ],
          link: `/indexes?instance=${INSTANCE_ID}&tab=indexes&focus=app.public.orders_customer_idx`,
        },
      ],
    },
    {
      kind: 'tables',
      total: 1,
      truncated: false,
      hits: [
        {
          key: 'app.public.orders',
          title: 'public.orders',
          subtitle: 'app',
          snippet: [
            { text: 'public.', match: false },
            { text: 'orders', match: true },
          ],
          link: `/indexes?instance=${INSTANCE_ID}&tab=dead-rows&focus=app.public.orders`,
        },
      ],
    },
    {
      kind: 'columns',
      total: 63,
      truncated: true,
      hits: Array.from({ length: 50 }, (_, i) => {
        const column = `orders_${String(i).padStart(2, '0')}`
        return {
          key: `app.public.archive.${column}`,
          title: `public.archive.${column}`,
          subtitle: 'integer · app',
          snippet: [
            { text: 'orders', match: true },
            { text: `_${String(i).padStart(2, '0')}`, match: false },
          ],
          link: `/indexes?instance=${INSTANCE_ID}&tab=dead-rows&focus=app.public.archive`,
        }
      }),
    },
  ],
}

/** A term nothing matches: every group is empty. */
export const empty: SearchResponse = {
  q: 'no-such-thing',
  groups: (['statements', 'indexes', 'tables', 'columns'] as const).map((kind) => ({
    kind,
    total: 0,
    truncated: false,
    hits: [],
  })),
}
