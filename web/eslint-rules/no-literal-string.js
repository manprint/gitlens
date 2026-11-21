/**
 * I18N-002 (plan 006 D11): visible text of a migrated area comes from the
 * catalog through t(). Flags JSX text, text props and table/column text
 * properties written as literals. Glossary terms that stay English (D8),
 * identifiers, units and text without letters pass.
 */

const TEXT_ATTRIBUTES = new Set([
  'aria-label',
  'aria-description',
  'aria-roledescription',
  'aria-valuetext',
  'alt',
  'ariaLabel',
  'caption',
  'description',
  'detailLabel',
  'emptyMessage',
  'emptyText',
  'heading',
  'hint',
  'label',
  'placeholder',
  'scope',
  'subject',
  'subtitle',
  'title',
  'tooltip',
])

const TEXT_PROPERTIES = new Set([
  'ariaLabel',
  'description',
  'emptyMessage',
  'emptyText',
  'header',
  'heading',
  'hint',
  'label',
  'placeholder',
  'subtitle',
  'title',
  'tooltip',
])

const CODE_ELEMENTS = new Set(['code', 'kbd', 'pre', 'samp'])

/** English on purpose: glossary D8, product names, units. */
const KEPT = new Set([
  'auto',
  'autovacuum',
  'checkpoint',
  'ctid',
  'deadlock',
  'h',
  'm',
  'min',
  'ms',
  's',
  'vacuum',
  'xmin',
  'B',
  'KiB',
  'MiB',
  'GiB',
  'TiB',
  'pg',
  'lens',
  'pglens',
  'PostgreSQL',
  'Postgres',
  'TimescaleDB',
  'EXPLAIN',
  'ANALYZE',
])

function isText(raw) {
  const text = raw.trim()
  if (!/[A-Za-z]/.test(text)) return false
  if (KEPT.has(text)) return false
  // Identifiers: setting names, pg_* views, functions, dotted paths.
  if (/^[A-Za-z_][\w]*[_.()][\w.()]*$/.test(text)) return false
  // Acronyms that stay English: SQL, WAL, PID, LSN, OID, CPU.
  if (/^[A-Z][A-Z0-9]{1,6}s?$/.test(text)) return false
  return true
}

function insideCode(node) {
  for (let parent = node.parent; parent; parent = parent.parent) {
    if (parent.type === 'JSXElement') {
      const name = parent.openingElement.name
      if (name.type === 'JSXIdentifier' && CODE_ELEMENTS.has(name.name)) return true
    }
  }
  return false
}

/** The literal text parts of an expression shown as-is. */
function literalParts(expression) {
  if (!expression) return []
  switch (expression.type) {
    case 'Literal':
      return typeof expression.value === 'string' ? [expression] : []
    case 'TemplateLiteral':
      return expression.quasis.filter((quasi) => isText(quasi.value.cooked ?? ''))
    case 'ConditionalExpression':
      return [...literalParts(expression.consequent), ...literalParts(expression.alternate)]
    case 'LogicalExpression':
      return literalParts(expression.right)
    default:
      return []
  }
}

function textOf(node) {
  if (node.type === 'TemplateElement') return node.value.cooked ?? ''
  if (node.type === 'Literal') return String(node.value)
  return ''
}

export default {
  meta: {
    type: 'problem',
    docs: { description: 'visible text comes from the i18n catalog (plan 006 I18N-002)' },
    messages: {
      literal: 'I18N-002: "{{text}}" is visible text; use t() with a catalog key',
    },
    schema: [],
  },
  create(context) {
    function report(node, text) {
      if (!isText(text) || insideCode(node)) return
      context.report({ node, messageId: 'literal', data: { text: text.trim().slice(0, 40) } })
    }
    function reportExpression(expression) {
      for (const part of literalParts(expression)) report(part, textOf(part))
    }
    return {
      JSXText(node) {
        report(node, node.value)
      },
      JSXAttribute(node) {
        const name = node.name.type === 'JSXIdentifier' ? node.name.name : null
        if (name === null || !TEXT_ATTRIBUTES.has(name) || node.value === null) return
        // <th scope="col"> is HTML, not text.
        if (name === 'scope' && node.value.type === 'Literal') return
        if (node.value.type === 'Literal') report(node.value, String(node.value.value))
        else if (node.value.type === 'JSXExpressionContainer')
          reportExpression(node.value.expression)
      },
      JSXExpressionContainer(node) {
        if (node.parent.type === 'JSXElement' || node.parent.type === 'JSXFragment') {
          reportExpression(node.expression)
        }
      },
      Property(node) {
        const key = node.key.type === 'Identifier' ? node.key.name : node.key.value
        if (!TEXT_PROPERTIES.has(key)) return
        reportExpression(node.value)
      },
    }
  },
}
