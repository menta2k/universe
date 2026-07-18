import { test, expect } from '@playwright/test'
import { login, unique } from './helpers'

// Critical flow: log in → register a machine → provision it.
// Requires DHCP to be enabled first (provision is gated on it, FR-016).
test('login, register a machine and provision it', async ({ page }) => {
  const id = unique()
  const name = `e2e-node-${id}`
  const mac = `52:54:00:e2:${id.slice(0, 2)}:${id.slice(2, 4)}`

  await login(page)

  // Enable DHCP (confirmation dialog).
  await page.getByRole('link', { name: /dhcp/i }).click()
  const toggle = page.getByRole('switch')
  if (!(await toggle.isChecked())) {
    await toggle.click()
    await page.getByRole('button', { name: /enable|confirm/i }).click()
  }

  // Register a machine.
  await page.getByRole('link', { name: /machines/i }).click()
  await page.getByRole('button', { name: /register machine/i }).click()
  await page.getByLabel(/mac/i).fill(mac)
  await page.getByLabel(/name/i).fill(name)
  await page.getByRole('button', { name: /save|create/i }).click()

  // The new machine appears in the table.
  await expect(page.getByText(name)).toBeVisible()

  // Provision it (confirm dialog) and expect the state to become "installing".
  const row = page.getByRole('row', { name: new RegExp(name) })
  await row.getByRole('button', { name: /provision/i }).click()
  await page.getByRole('button', { name: /provision|confirm/i }).click()
  await expect(row.getByText(/installing/i)).toBeVisible()
})
