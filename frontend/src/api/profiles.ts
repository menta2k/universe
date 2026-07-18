/**
 * ProfileService client (list only — full CRUD arrives with the Profiles
 * page). Used by the machine dialog to populate the profile select.
 */
import { request } from './http'
import type { Profile } from './types'

interface WireProfileList {
  readonly profiles?: readonly Profile[]
}

export async function listProfiles(): Promise<readonly Profile[]> {
  const data = await request<WireProfileList>('/api/v1/profiles')
  return data.profiles ?? []
}
