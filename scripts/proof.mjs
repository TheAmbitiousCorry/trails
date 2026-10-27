// Run from frontend/ (make proof) so playwright resolves from frontend/node_modules.
import { createRequire } from 'node:module'
import { mkdirSync } from 'node:fs'

const require = createRequire(process.cwd() + '/package.json')
const { chromium } = require('playwright')

const addr = process.env.ADDR || '127.0.0.1:7777'
const out = process.env.OUT || '.proof'
const base = `http://${addr}`
const widths = [1280, 390]

mkdirSync(out, { recursive: true })

const getJSON = async (path) => {
  const r = await fetch(base + path)
  if (!r.ok) throw new Error(`${path}: HTTP ${r.status}`)
  return r.json()
}

const graph = await getJSON('/api/graph')
const sessions = (await getJSON('/api/sessions')).sessions || []
const sign = graph.nodes.find((n) => n.type === 'sign' && n.id !== 'root') || graph.nodes.find((n) => n.type === 'sign')
const entry = graph.nodes.find((n) => n.type === 'entry')
const session = sessions[0]

const shots = [
  ['map', '/'],
  ['graph', '/graph'],
  sign && ['graph-local', `/graph?focus=${encodeURIComponent(sign.id)}&depth=1`],
  session && ['graph-session', `/graph?session=${encodeURIComponent(session.id)}`],
  session && ['session-trail', `/sessions/${encodeURIComponent(session.id)}`],
  sign && ['sign-form', `/signs/${encodeURIComponent(sign.id)}`, 'form'],
  sign && ['sign-refusal', `/signs/${encodeURIComponent(sign.id)}`, 'refusal'],
  entry && ['entry', `/entries/${encodeURIComponent(entry.id)}`],
  ['sessions', '/sessions'],
  ['gaps', '/gaps'],
  ['provisional', '/provisional'],
  ['tags', '/tags'],
  ['logs', '/logs'],
  ['menu', '/', 'menu'],
].filter(Boolean)

const problems = []
const written = []
const browser = await chromium.launch()

for (const width of widths) {
  const ctx = await browser.newContext({ viewport: { width, height: 800 } })
  for (const [name, path, mode] of shots) {
    if (mode === 'menu' && width > 700) continue
    const page = await ctx.newPage()
    const tag = `${name}-${width}`
    page.on('pageerror', (e) => problems.push(`${tag}: page error: ${e.message}`))
    page.on('console', (m) => { // The refusal shot provokes a 400 on purpose; the browser logs it as a console error.
      if (m.type() === 'error' && !(mode === 'refusal' && /status of 400/.test(m.text()))) problems.push(`${tag}: console error: ${m.text()}`) })
    try {
      await page.goto(base + path, { waitUntil: 'networkidle' })
      await page.waitForTimeout(name.startsWith('graph') ? 2500 : 300)
      if (mode === 'form' || mode === 'refusal') {
        const title = page.locator('#new-sign-title')
        await title.scrollIntoViewIfNeeded()
        if (mode === 'refusal') {
          // The title input is required; bypass native validation so the server's refusal renders.
          await page.locator('form.add-sign').evaluate((f) => { f.noValidate = true })
          await page.getByRole('button', { name: 'Add sign' }).click()
          await page.locator('#new-sign-error').waitFor({ timeout: 5000 })
        }
        await page.locator('#add-sign').scrollIntoViewIfNeeded()
      }
      if (mode === 'menu') {
        await page.getByRole('button', { name: 'Menu' }).click()
        await page.waitForTimeout(300)
      }
      const file = `${out}/${tag}.png`
      await page.screenshot({ path: file })
      written.push(file)
    } catch (e) {
      problems.push(`${tag}: ${e.message}`)
    }
    await page.close()
  }
  await ctx.close()
}
await browser.close()

console.log(`wrote ${written.length} files:\n${written.join('\n')}`)
if (problems.length) {
  console.error(`\n${problems.length} problem(s):\n${problems.join('\n')}`)
  process.exit(1)
}
