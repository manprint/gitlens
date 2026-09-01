import { describe, expect, it } from 'vitest'

import { parseChunkList, parseStaticImports } from '../../scripts/bundle-budget'

describe('bundle budget parser', () => {
  it('UI-PERF-001 parses the measured chunk list', () => {
    expect(parseChunkList('index.js\t404531\t125677\npages.js\t1633687\t519063\n')).toEqual([
      { fileName: 'index.js', bytes: 404531, gzipBytes: 125677 },
      { fileName: 'pages.js', bytes: 1633687, gzipBytes: 519063 },
    ])
  })

  it('UI-PERF-002 keeps dynamic imports out of synchronous dependencies', () => {
    expect(parseStaticImports('import "./shared.js"; import("./lazy.js");')).toEqual([
      './shared.js',
    ])
  })
})
