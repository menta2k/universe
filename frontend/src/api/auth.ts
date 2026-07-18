/**
 * AuthService client: login / logout / me over session cookies.
 * These calls opt out of the global 401 redirect — auth flows handle
 * unauthenticated states themselves (login errors, router guard).
 */
import { request } from './http'
import type { Operator } from './types'

export interface LoginRequest {
  readonly username: string
  readonly password: string
}

export function login(credentials: LoginRequest): Promise<Operator> {
  return request<Operator>('/api/v1/auth/login', {
    method: 'POST',
    body: credentials,
    skipAuthRedirect: true,
  })
}

export function logout(): Promise<void> {
  return request<void>('/api/v1/auth/logout', {
    method: 'POST',
    skipAuthRedirect: true,
  })
}

export function me(): Promise<Operator> {
  return request<Operator>('/api/v1/auth/me', { skipAuthRedirect: true })
}
