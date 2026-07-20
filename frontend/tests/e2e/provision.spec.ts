import { test, expect } from '@playwright/test'
import { login, unique } from './helpers'

// Critical flow: log in → enable DHCP (with a subnet) → create a profile →
// register a machine with it → provision it. Provisioning is gated on DHCP
// being enabled (FR-016) and requires an assigned profile.
test('login, register a machine and provision it', async ({ page }) => {
  const id = unique()
  const name = `e2e-node-${id}`
  const profileName = `e2e-prof-${id}`
  const mac = `52:54:00:e2:${id.slice(0, 2)}:${id.slice(2, 4)}`

  await login(page)

  // Enable DHCP. The Vuetify switch input carries the enable-switch testid;
  // enabling requires at least one configured subnet.
  await page.getByRole('link', { name: /dhcp/i }).click()
  // Wait for the config to load before interacting with the switch.
  await expect(page.getByText(/config version/i)).toBeVisible()
  // The testid lands on the v-switch wrapper; the real checkbox is inside.
  const toggle = page.getByTestId('enable-switch')
  const toggleInput = toggle.locator('input[type="checkbox"]')
  if (!(await toggleInput.isChecked())) {
    if (!(await page.getByTestId('subnet-row-0').isVisible())) {
      await page.getByTestId('add-subnet-btn').click()
      await page.getByLabel(/network \(cidr\)/i).fill('10.99.0.0/24')
      await page.getByLabel(/gateway/i).fill('10.99.0.1')
      await page.getByLabel(/range start/i).fill('10.99.0.100')
      await page.getByLabel(/range end/i).fill('10.99.0.200')
      await page.getByTestId('save-config-btn').click()
      await expect(page.getByText(/configuration saved/i)).toBeVisible()
    }
    await toggle.click()
    await expect(page.getByRole('dialog')).toBeVisible()
    await page.getByRole('button', { name: /enable dhcp/i }).click()
    await expect(page.getByText(/running/i)).toBeVisible()
  }

  // Create a profile — provisioning requires one on the machine.
  await page.getByRole('link', { name: /profiles/i }).click()
  await page.getByTestId('new-profile-btn').click()
  await page.getByTestId('field-name').locator('input').fill(profileName)
  await page
    .getByTestId('field-ssh-0')
    .locator('textarea')
    .first()
    .fill('ssh-ed25519 AAAAe2etestkey e2e@ci')
  await page.getByTestId('save-btn').click()
  await expect(page.getByText(profileName)).toBeVisible()

  // Register a machine with that profile.
  await page.getByRole('link', { name: /machines/i }).click()
  await page.getByRole('button', { name: /register machine/i }).click()
  await page.getByTestId('field-mac').locator('input').fill(mac)
  await page.getByTestId('field-name').locator('input').fill(name)
  await page.getByTestId('field-profile').click()
  await page.getByRole('option', { name: profileName }).click()
  await page.getByTestId('save-btn').click()

  // The new machine appears in the table.
  await expect(page.getByText(name)).toBeVisible()

  // Provision it (confirm dialog) and expect the state to become "installing".
  const row = page.getByRole('row', { name: new RegExp(name) })
  await row.getByRole('button', { name: /provision/i }).click()
  await page
    .getByRole('dialog')
    .getByRole('button', { name: /provision|confirm/i })
    .click()
  await expect(row.getByText(/installing/i)).toBeVisible()
})
