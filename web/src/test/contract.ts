import Ajv, { type AnySchema, type ValidateFunction } from 'ajv'
import addFormats from 'ajv-formats'
import { readFileSync } from 'fs'
import { resolve } from 'path'
import { cwd } from 'process'
import { parse } from 'yaml'

const CONTRACT_URI = 'https://pglens.test/openapi.yaml'

type JsonRecord = Record<string, unknown>

interface ResponseSchema {
  content?: { 'application/json'?: { schema?: unknown } }
}

interface Operation {
  operationId?: string
  responses?: Record<string, ResponseSchema>
}

interface OpenApiDocument {
  paths: Record<string, Record<string, unknown>>
}

let document: OpenApiDocument | undefined
let ajv: Ajv | undefined
const validators = new Map<string, ValidateFunction>()

function loadContract() {
  if (document && ajv) {
    return { document, ajv }
  }

  // The contract is kept beside the web workspace and is read once per Vitest process.
  document = parse(readFileSync(resolve(cwd(), '../api/openapi.yaml'), 'utf8')) as OpenApiDocument
  ajv = new Ajv({ allErrors: true, strict: false })
  addFormats(ajv)
  ajv.addSchema(document, CONTRACT_URI)
  return { document, ajv }
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isOperation(value: unknown): value is Operation {
  return (
    isRecord(value) && (value.operationId === undefined || typeof value.operationId === 'string')
  )
}

function findOperation(operationId: string): Operation {
  const { document: contract } = loadContract()
  for (const pathItem of Object.values(contract.paths)) {
    for (const operation of Object.values(pathItem)) {
      if (isOperation(operation) && operation.operationId === operationId) {
        return operation
      }
    }
  }
  throw new Error(`Unknown OpenAPI operationId: ${operationId}`)
}

function absoluteRefs(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(absoluteRefs)
  }
  if (!isRecord(value)) {
    return value
  }

  const result: JsonRecord = {}
  for (const [key, child] of Object.entries(value)) {
    result[key] =
      key === '$ref' && typeof child === 'string' && child.startsWith('#/')
        ? `${CONTRACT_URI}${child}`
        : absoluteRefs(child)
  }
  return result
}

function validatorFor(operationId: string, statusCode: number): ValidateFunction | undefined {
  const operation = findOperation(operationId)
  const response = operation.responses?.[String(statusCode)]
  if (!response) {
    throw new Error(`OpenAPI operation ${operationId} has no response ${statusCode}`)
  }

  const schema = response.content?.['application/json']?.schema
  if (schema === undefined) {
    return undefined
  }

  const cacheKey = `${operationId}:${statusCode}`
  const cached = validators.get(cacheKey)
  if (cached) {
    return cached
  }

  const contract = loadContract()
  const validator = contract.ajv.compile(absoluteRefs(schema) as AnySchema)
  validators.set(cacheKey, validator)
  return validator
}

export function assertMatchesContract(
  operationId: string,
  statusCode: number,
  body: unknown,
): void {
  const validator = validatorFor(operationId, statusCode)
  if (!validator) {
    if (body === undefined) {
      return
    }
    throw new Error(`OpenAPI response ${operationId} ${statusCode} has no JSON schema`)
  }

  if (validator(body)) {
    return
  }

  const errors = (validator.errors ?? []).map(
    (error) => `${error.instancePath || '/'} ${error.message ?? 'is invalid'}`,
  )
  throw new Error(
    `OpenAPI contract violation for ${operationId} ${statusCode}:\n${errors.join('\n')}`,
  )
}
