import { test, expect } from '@playwright/test'
import { login } from './helpers'

// Critical flow: the profile editor surfaces server-side validation errors
// (a profile with no SSH key must be rejected — Constitution IV / FR-008).
test('profile editor blocks a profile with no SSH key', async ({ page }) => {
  await login(page)
  await page.getByRole('link', { name: /profiles/i }).click()
  await page.getByRole('button', { name: /new profile/i }).click()

  // Match the profile name exactly: the editor also carries a "Login username"
  // field, so a loose /name/i matches two textboxes and trips strict mode.
  await page.getByRole('textbox', { name: 'Profile name' }).fill(`bad-profile-${Date.now()}`)
  // Leave SSH authorized keys empty.
  await page.getByRole('button', { name: /save|create/i }).click()

  // Client- or server-side validation must flag the missing key and keep the
  // dialog open (nothing was persisted).
  await expect(page.getByText(/ssh.*key.*required|at least one ssh key/i)).toBeVisible()
  await expect(page.getByRole('dialog')).toBeVisible()
})
