import { type Page, expect } from '@playwright/test'

// Credentials come from the backend bootstrap_operator config.
export const ADMIN_USER = process.env.E2E_USER ?? 'admin'
export const ADMIN_PASS = process.env.E2E_PASS ?? 'change-me-please'

/** Log in through the UI and land on the authenticated shell. */
export async function login(page: Page): Promise<void> {
  await page.goto('/login')
  await page.getByLabel(/username/i).fill(ADMIN_USER)
  await page.getByLabel(/password/i).fill(ADMIN_PASS)
  await page.getByRole('button', { name: /log ?in|sign ?in/i }).click()
  await expect(page).toHaveURL(/\/$|\/dashboard|\/machines|\/$/)
}

/** Unique suffix so repeated runs don't collide on unique names/MACs. */
export function unique(): string {
  return Math.random().toString(16).slice(2, 8)
}
