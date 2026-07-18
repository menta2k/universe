/**
 * Typed fetch wrapper for the Netboot Manager admin API.
 *
 * Every backend response uses the envelope `{success, data, error, meta?}`.
 * This module unwraps it, surfaces error strings as `ApiError`, and triggers
 * a redirect to /login on 401 responses (unless explicitly skipped).
 */

export interface ApiMeta {
  readonly total: number
  readonly page: number
  readonly page_size: number
}

/** Structured error emitted by the backend for typed failures (e.g. 422). */
interface EnvelopeErrorObject {
  readonly reason?: string
  readonly message?: string
  readonly details?: Readonly<Record<string, string>>
}

interface Envelope<T> {
  readonly success: boolean
  readonly data: T | null
  readonly error: string | EnvelopeErrorObject | null
  readonly meta?: ApiMeta
}

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    /** Per-field validation messages from 422 responses (field -> message). */
    readonly details?: Readonly<Record<string, string>>,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

export type QueryParams = Readonly<Record<string, string | number | boolean | undefined>>

export interface RequestOptions {
  readonly method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  readonly body?: unknown
  readonly query?: QueryParams
  /** Suppress the 401 -> /login redirect (used by auth endpoints themselves). */
  readonly skipAuthRedirect?: boolean
}

type UnauthorizedHandler = () => void

const defaultUnauthorizedHandler: UnauthorizedHandler = () => {
  if (window.location.pathname !== '/login') {
    window.location.assign('/login')
  }
}

let unauthorizedHandler: UnauthorizedHandler = defaultUnauthorizedHandler

/** Override the 401 handler (pass null to restore the default). Used by tests and the router. */
export function setUnauthorizedHandler(handler: UnauthorizedHandler | null): void {
  unauthorizedHandler = handler ?? defaultUnauthorizedHandler
}

function buildUrl(path: string, query?: QueryParams): string {
  if (!query) return path
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined) params.set(key, String(value))
  }
  const qs = params.toString()
  return qs.length > 0 ? `${path}?${qs}` : path
}

function isEnvelope(value: unknown): value is Envelope<unknown> {
  return typeof value === 'object' && value !== null && 'success' in value
}

async function parseEnvelope(response: Response): Promise<Envelope<unknown>> {
  let parsed: unknown
  try {
    parsed = await response.json()
  } catch {
    throw new ApiError(`Unexpected response from server (HTTP ${response.status})`, response.status)
  }
  if (!isEnvelope(parsed)) {
    throw new ApiError(`Malformed response envelope (HTTP ${response.status})`, response.status)
  }
  return parsed
}

async function execute(path: string, options: RequestOptions): Promise<Envelope<unknown>> {
  const init: RequestInit = {
    method: options.method ?? 'GET',
    credentials: 'include',
    headers: { Accept: 'application/json' },
  }
  if (options.body !== undefined) {
    init.headers = { ...init.headers, 'Content-Type': 'application/json' }
    init.body = JSON.stringify(options.body)
  }

  let response: Response
  try {
    response = await fetch(buildUrl(path, options.query), init)
  } catch {
    throw new ApiError('Network error: could not reach the server', 0)
  }

  if (response.status === 401 && !options.skipAuthRedirect) {
    unauthorizedHandler()
  }

  const envelope = await parseEnvelope(response)
  if (!response.ok || !envelope.success) {
    throw toApiError(envelope.error, response.status)
  }
  return envelope
}

/** Build an ApiError from either the legacy string error or the structured object form. */
function toApiError(error: string | EnvelopeErrorObject | null, status: number): ApiError {
  const fallback = `Request failed (HTTP ${status})`
  if (error === null) return new ApiError(fallback, status)
  if (typeof error === 'string') return new ApiError(error, status)
  const message = error.message || error.reason || fallback
  const details =
    error.details && Object.keys(error.details).length > 0 ? { ...error.details } : undefined
  return new ApiError(message, status, details)
}

/** Perform a request and return the unwrapped `data` payload. */
export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const envelope = await execute(path, options)
  return envelope.data as T
}

export interface MultipartOptions {
  readonly method?: 'POST' | 'PUT'
  readonly skipAuthRedirect?: boolean
}

/**
 * Perform a raw multipart/form-data upload. The browser sets the multipart
 * boundary automatically, so `Content-Type` is intentionally left unset.
 * The response envelope is unwrapped exactly like JSON requests, so 422 field
 * details surface as `ApiError.details`.
 */
export async function requestMultipart<T>(
  path: string,
  form: FormData,
  options: MultipartOptions = {},
): Promise<T> {
  let response: Response
  try {
    response = await fetch(path, {
      method: options.method ?? 'POST',
      credentials: 'include',
      headers: { Accept: 'application/json' },
      body: form,
    })
  } catch {
    throw new ApiError('Network error: could not reach the server', 0)
  }

  if (response.status === 401 && !options.skipAuthRedirect) {
    unauthorizedHandler()
  }

  const envelope = await parseEnvelope(response)
  if (!response.ok || !envelope.success) {
    throw toApiError(envelope.error, response.status)
  }
  return envelope.data as T
}

/** Perform a request and return both `data` and pagination `meta`. */
export async function requestWithMeta<T>(
  path: string,
  options: RequestOptions = {},
): Promise<{ data: T; meta?: ApiMeta }> {
  const envelope = await execute(path, options)
  return { data: envelope.data as T, meta: envelope.meta }
}
