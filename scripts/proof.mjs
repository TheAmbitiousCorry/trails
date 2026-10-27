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

const total = (s) => Object.values(s.counts).reduce((a, b) => a + b, 0)

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
// The replay shots want the longest walk, so the scrubber has hops to move through.
const busiest = sessions.reduce((a, b) => (total(b) > total(a) ? b : a), sessions[0])

const shots = [
  ['map', '/'],
  ['graph-ridge', '/graph', 'ridge'],
  ['graph-topo', '/graph', 'topo'],
  ['graph-cluster', '/graph', 'cluster'],
  sign && ['graph-local', `/graph?focus=${encodeURIComponent(sign.id)}`, 'ridge'],
  session && ['session-trail', `/sessions/${encodeURIComponent(session.id)}`],
  busiest && ['session-replay-mid', `/sessions/${encodeURIComponent(busiest.id)}`, 'replay-mid'],
  busiest && ['session-replay-end', `/sessions/${encodeURIComponent(busiest.id)}`, 'replay-end'],
  busiest && ['session-replay-keys', `/sessions/${encodeURIComponent(busiest.id)}`, 'replay-keys'],
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

// Returns the first pair of visible labels whose boxes intersect, or null.
// Opacity is the product up the tree so dimmed nodes are skipped; boxes are the getBBox rectangle mapped through the screen matrix, because a rotated Topo label's axis-aligned getBoundingClientRect covers its neighbours.
const overlappingLabels = () => {
  const seen = []
  for (const t of document.querySelectorAll('svg text')) {
    let opacity = 1
    for (let el = t; el && el !== document.body; el = el.parentElement) opacity *= Number(getComputedStyle(el).opacity)
    const b = t.getBBox()
    const m = t.getScreenCTM()
    if (opacity <= 0.5 || !t.textContent.trim() || !b.width || !b.height || !m) continue
    const quad = [[b.x, b.y], [b.x + b.width, b.y], [b.x + b.width, b.y + b.height], [b.x, b.y + b.height]]
      .map(([x, y]) => [m.a * x + m.c * y + m.e, m.b * x + m.d * y + m.f])
    seen.push({ text: t.textContent.trim(), quad })
  }
  const slack = 1
  const axes = (q) => [0, 1].map((i) => { const dx = q[i + 1][0] - q[i][0], dy = q[i + 1][1] - q[i][1], len = Math.hypot(dx, dy); return [-dy / len, dx / len] })
  const span = (q, [ax, ay]) => { const d = q.map(([x, y]) => x * ax + y * ay); return [Math.min(...d), Math.max(...d)] }
  const hit = (p, q) => [...axes(p), ...axes(q)].every((axis) => {
    const [a0, a1] = span(p, axis), [b0, b1] = span(q, axis)
    return a0 + slack < b1 && b0 + slack < a1
  })
  for (let i = 0; i < seen.length; i++) {
    for (let j = i + 1; j < seen.length; j++) {
      if (hit(seen[i].quad, seen[j].quad)) return [seen[i].text, seen[j].text]
    }
  }
  return null
}

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
      if (name.startsWith('graph')) {
        if (mode === 'ridge' || mode === 'topo' || mode === 'cluster') {
          await page.getByRole('button', { name: mode[0].toUpperCase() + mode.slice(1), exact: true }).click()
          await page.waitForTimeout(600)
        }
        const pair = await page.evaluate(overlappingLabels)
        if (pair) problems.push(`${tag}: labels overlap: "${pair[0]}" and "${pair[1]}"`)
      }
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
      if (mode?.startsWith('replay')) {
        const count = page.getByTestId('hop-count')
        const hop = async () => Number((await count.innerText()).split(' ')[0])
        const ofTotal = Number((await count.innerText()).split(' ')[2])
        if (mode === 'replay-end' && (await hop()) !== ofTotal) problems.push(`${tag}: replay did not open on its last hop`)
        if (mode === 'replay-mid') {
          await page.getByRole('button', { name: 'Play', exact: true }).click()
          await page.waitForTimeout(2000)
          const now = await hop()
          if (!(now > 0 && now < ofTotal)) problems.push(`${tag}: after 2 s of Play the hop is ${now} of ${ofTotal}, expected part way`)
        }
        if (mode === 'replay-keys') {
          const slider = page.getByRole('slider', { name: 'Hop in the walk' })
          await slider.focus()
          const before = await hop()
          await page.keyboard.press('ArrowLeft')
          const left = await hop()
          await page.keyboard.press('ArrowRight')
          const right = await hop()
          if (left !== before - 1 || right !== before) problems.push(`${tag}: arrow keys moved the hop ${before} to ${left} to ${right}, expected one step each way`)
          await page.keyboard.press('ArrowLeft')
          await page.waitForTimeout(300)
        }
        await page.locator('.replay').scrollIntoViewIfNeeded()
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
