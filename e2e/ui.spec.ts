import { expect, test, type Page } from '@playwright/test'
import { createBook, apiAs, bookPayload, signInPage, uniqueEmail } from './helpers'

// Browser smoke over the React app. Selectors are role + accessible name,
// matching the labels the web app uses; regexes keep them tolerant of
// punctuation and casing while the UI evolves.

async function fillBookForm(page: Page, fields: Record<string, unknown>): Promise<void> {
  const text: [RegExp, unknown][] = [
    [/^title$/i, fields.title],
    [/^author$/i, fields.author],
    [/^isbn$/i, fields.isbn],
    [/^year$/i, fields.year],
    [/^genre$/i, fields.genre],
    [/^publisher$/i, fields.publisher],
    [/^copies$/i, fields.copies],
    [/^description$/i, fields.description],
    [/^cover url$/i, fields.coverUrl],
    [/^tags$/i, (fields.tags as string[]).join(', ')],
  ]
  for (const [label, value] of text) {
    if (value === undefined || value === null) continue
    await page.getByLabel(label).fill(String(value))
  }
}

test('the dev sign-in form signs a librarian in and the header shows their name', async ({ page }) => {
  await page.goto('/login')
  const form = page.locator('form').first()
  await expect(form).toBeVisible()
  await form.getByLabel(/^email$/i).fill(uniqueEmail('ui'))
  await form.getByLabel(/^name$/i).fill('Hypatia Librarian')
  await form.getByLabel(/^role$/i).selectOption('librarian')
  await form.getByRole('button', { name: /sign in \(dev\)/i }).click()
  await expect(page.getByRole('banner')).toContainText(/Hypatia Librarian/, { timeout: 15_000 })
})

test('a librarian adds a book through the form and lands on its page', async ({ page }) => {
  await signInPage(page, { email: uniqueEmail('ui-lib'), name: 'Form Librarian', role: 'librarian' })
  await page.getByRole('link', { name: /add book/i }).first().click()
  await expect(page).toHaveURL(/\/books\/new/, { timeout: 15_000 })

  const fields = bookPayload()
  await fillBookForm(page, fields)
  await page.getByRole('button', { name: /save book/i }).click()

  await expect(page).toHaveURL(/\/books\/[A-Za-z0-9]+$/, { timeout: 15_000 })
  await expect(page.getByRole('heading', { name: fields.title as string })).toBeVisible()
})

test('the catalog search finds a book by title and shows its card', async ({ page, baseURL }) => {
  const librarian = await apiAs(baseURL!, { email: uniqueEmail('ui-search'), name: 'Search Librarian', role: 'librarian' })
  const book = await createBook(librarian)
  await librarian.dispose()

  await page.goto('/')
  const search = page.getByRole('searchbox', { name: /search the catalog/i }).or(
    page.getByRole('textbox', { name: /search the catalog/i }),
  )
  await search.first().fill(book.title)
  await search.first().press('Enter')

  const card = page.locator('[data-testid="book-card"]').filter({ hasText: book.title })
  await expect(card.first()).toBeVisible({ timeout: 15_000 })
  await expect(card.first().getByRole('link', { name: book.title })).toBeVisible()
})

test('a member checks a book out and sees Return', async ({ page, baseURL }) => {
  const librarian = await apiAs(baseURL!, { email: uniqueEmail('ui-loanlib'), name: 'Loan Librarian', role: 'librarian' })
  const book = await createBook(librarian, { copies: 1 })
  await librarian.dispose()

  await signInPage(page, { email: uniqueEmail('ui-member'), name: 'Borrowing Member', role: 'member' }, `/books/${book.id}`)
  await expect(page.getByRole('heading', { name: book.title })).toBeVisible({ timeout: 15_000 })
  await page.getByRole('button', { name: /^check out$/i }).click()
  await expect(page.getByRole('region', { name: /actions/i }).getByRole('button', { name: /^return$/i })).toBeVisible({ timeout: 15_000 })
})

test('the dashboard shows stat tiles for a librarian', async ({ page }) => {
  await signInPage(page, { email: uniqueEmail('ui-dash'), name: 'Dash Librarian', role: 'librarian' }, '/dashboard')
  await expect(page.getByRole('heading', { name: /dashboard/i }).first()).toBeVisible({ timeout: 15_000 })
  for (const label of [/books/i, /open loans/i, /overdue/i]) {
    await expect(page.getByText(label).first()).toBeVisible()
  }
})

test('/users is forbidden for a librarian', async ({ page }) => {
  await signInPage(page, { email: uniqueEmail('ui-forbidden'), name: 'Not An Admin', role: 'librarian' }, '/users')
  await expect(page.getByText(/403|forbidden|not allowed|permission/i).first()).toBeVisible({ timeout: 15_000 })
})

test('terms and privacy pages are public', async ({ page }) => {
  await page.goto('/tos')
  await expect(page.getByRole('heading', { level: 1, name: /borrowing from/i })).toBeVisible()
  await page.goto('/privacy')
  await expect(page.getByRole('heading', { level: 1, name: /keeps about you/i })).toBeVisible()
  await expect(page.getByRole('navigation', { name: 'Legal' }).getByRole('link', { name: 'Terms' })).toBeVisible()
})
