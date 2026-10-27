// Run from frontend/ (make proof) so playwright resolves from frontend/node_modules.
import { createRequire } from 'node:module'
import { mkdirSync } from 'node:fs'

const require = createRequire(process.cwd() + '/package.json')
const { chromium } = require('playwright')
const { polygonContains } = await import(require.resolve('d3-polygon'))

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

// PROJECT names the project whose pages are captured; the project select is captured whatever it is.
const project = process.env.PROJECT || (await getJSON('/api/projects')).projects[0]?.id
if (!project) throw new Error('the server at ' + addr + ' holds no project')
const api = `/api/p/${encodeURIComponent(project)}`
const at = (path) => `/p/${encodeURIComponent(project)}${path === '/' ? '' : path}`

const graph = await getJSON(`${api}/graph`)
const sessions = (await getJSON(`${api}/sessions`)).sessions || []
const sign = graph.nodes.find((n) => n.type === 'sign' && n.id !== 'root') || graph.nodes.find((n) => n.type === 'sign')
const entry = graph.nodes.find((n) => n.type === 'entry')
const session = sessions[0]
// An unresolved conflicting pair: a verified original and the provisional copy an agent stored beside it. A server holding none skips the two shots.
const tierOf = (id) => graph.nodes.find((n) => n.id === id)?.tier
const conflict = graph.edges.filter((e) => e.relation === 'conflicts_with')
  .map((e) => ({ a: e.from, b: e.to }))
  .map(({ a, b }) => (tierOf(a) === 'verified' && tierOf(b) === 'provisional' ? { original: a, copy: b } : tierOf(b) === 'verified' && tierOf(a) === 'provisional' ? { original: b, copy: a } : null))
  .find(Boolean)
// The replay shots want the longest walk, so the scrubber has hops to move through.
const busiest = sessions.reduce((a, b) => (total(b) > total(a) ? b : a), sessions[0])

const shots = [
  ['map', at('/')],
  ['graph-ridge', at('/graph'), 'ridge'],
  ['graph-topo', at('/graph'), 'topo'],
  ['graph-cluster', at('/graph'), 'cluster'],
  sign && ['graph-local', at(`/graph?focus=${encodeURIComponent(sign.id)}`), 'ridge'],
  session && ['session-trail', at(`/sessions/${encodeURIComponent(session.id)}`)],
  busiest && ['session-replay-mid', at(`/sessions/${encodeURIComponent(busiest.id)}`), 'replay-mid'],
  busiest && ['session-replay-end', at(`/sessions/${encodeURIComponent(busiest.id)}`), 'replay-end'],
  busiest && ['session-replay-keys', at(`/sessions/${encodeURIComponent(busiest.id)}`), 'replay-keys'],
  sign && ['sign-form', at(`/signs/${encodeURIComponent(sign.id)}`), 'form'],
  sign && ['sign-refusal', at(`/signs/${encodeURIComponent(sign.id)}`), 'refusal'],
  entry && ['entry', at(`/entries/${encodeURIComponent(entry.id)}`)],
  conflict && ['entry-conflict-original', at(`/entries/${encodeURIComponent(conflict.original)}`), 'supersede'],
  conflict && ['entry-conflict-copy', at(`/entries/${encodeURIComponent(conflict.copy)}`), 'supersede'],
  ['sessions', at('/sessions')],
  ['gaps', at('/gaps')],
  ['provisional', at('/provisional')],
  ['tags', at('/tags')],
  ['logs', at('/logs')],
  ['project-select', at('/'), 'select'],
  ['menu', at('/'), 'menu'],
].filter(Boolean)

// Returns the first pair of visible labels whose boxes intersect, or null.
// Opacity is the product up the tree so dimmed nodes are skipped; boxes are the getBBox rectangle mapped through the screen matrix, because a rotated Topo label's axis-aligned getBoundingClientRect covers its neighbours.
// The map is measured at its own scale: Chrome rounds the font metrics of SVG text under a strong zoom-out, which inflates boxes on a phone-sized view, so the zoom is lifted for the measurement and put back.
const overlappingLabels = () => {
  const canvas = document.querySelector('.trailmap svg > g')
  const zoomed = canvas?.getAttribute('transform')
  canvas?.setAttribute('transform', 'translate(0 0) scale(1)')
  const overlapAtScale = () => {
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
  try {
    return overlapAtScale()
  } finally {
    if (zoomed) canvas.setAttribute('transform', zoomed)
  }
}

// Cluster: island hulls must not touch and the trailhead must sit in none. Each hull's outline (its vertices and points along each edge) is tested against every other hull.
const clusterProblem = (hulls, head) => {
  const outline = (h) => h.flatMap((a, i) => {
    const b = h[(i + 1) % h.length]
    return Array.from({ length: 12 }, (_, k) => [a[0] + ((b[0] - a[0]) * k) / 12, a[1] + ((b[1] - a[1]) * k) / 12])
  })
  for (let i = 0; i < hulls.length; i++) {
    for (let j = 0; j < hulls.length; j++) {
      if (i !== j && outline(hulls[i]).some((q) => polygonContains(hulls[j], q))) return `island hulls ${i} and ${j} intersect`
    }
  }
  if (!head) return 'no trailhead on the Cluster map'
  const covered = hulls.findIndex((h) => polygonContains(h, head))
  return covered >= 0 ? `the trailhead sits inside island hull ${covered}` : null
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
        if (mode === 'cluster') {
          // polygonContains is d3-polygon's; the page cannot import it, so the hull points come out and the test runs here.
          const hulls = await page.evaluate(() => [...document.querySelectorAll('.tm-hulls path')].map((el) => el.getAttribute('d').slice(1, -1).split('L').map((q) => q.split(',').map(Number))))
          const head = await page.evaluate(() => {
            const m = document.querySelector('.tm-trailhead')?.parentElement?.getAttribute('transform')?.match(/translate\(([^,]+),([^)]+)\)/)
            return m ? [Number(m[1]), Number(m[2])] : null
          })
          const hit = clusterProblem(hulls, head)
          if (hit) problems.push(`${tag}: ${hit}`)
        }
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
      if (mode === 'supersede') {
        const button = page.getByRole('button', { name: /^Supersede / })
        if ((await button.count()) !== 1) problems.push(`${tag}: expected one Supersede button, found ${await button.count()}`)
        else await button.scrollIntoViewIfNeeded()
      }
      if (mode === 'select') {
        if (width <= 700) { await page.getByRole('button', { name: 'Menu' }).click(); await page.waitForTimeout(300) }
        const select = page.getByRole('combobox', { name: 'Project' })
        if ((await select.locator('option').count()) < 1) problems.push(`${tag}: the project select lists no project`)
        await select.focus()
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
