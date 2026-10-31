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
// A project holding no staged entries skips the entry badge shot; the Staged page itself is captured either way, so its empty state is a project with none.
const stagedEntry = (await getJSON(`${api}/staged`)).branches.flatMap((b) => b.entries)[0]
// An unresolved conflicting pair: a verified original and the provisional copy an agent stored beside it. A server holding none skips the two shots.
const tierOf = (id) => graph.nodes.find((n) => n.id === id)?.tier
const conflict = graph.edges.filter((e) => e.relation === 'conflicts_with')
  .map((e) => ({ a: e.from, b: e.to }))
  .map(({ a, b }) => (tierOf(a) === 'verified' && tierOf(b) === 'provisional' ? { original: a, copy: b } : tierOf(b) === 'verified' && tierOf(a) === 'provisional' ? { original: b, copy: a } : null))
  .find(Boolean)
// The edit shots want an entry the browser may edit (not staged) that holds anchors and tags, so the form's rows are filled; and, for the staged-copy warning, one with a staged copy.
// A server holding none skips the shots. The conflict shot saves a change from the script mid-edit, so it changes that entry's body.
const stagedIds = new Set((await getJSON(`${api}/staged`)).branches.flatMap((b) => b.entries.map((e) => e.id)))
const details = []
for (const n of graph.nodes.filter((n) => n.type === 'entry' && !stagedIds.has(n.id))) details.push(await getJSON(`${api}/entries/${encodeURIComponent(n.id)}`))
const editable = details.filter((d) => !d.entry.body_problem)
const editTarget = (editable.find((d) => d.form_anchors.length && d.entry.tags.length && d.entry.kind !== 'decision' && !d.staged_copies.length) || editable.find((d) => d.form_anchors.length))?.entry.id
const copyTarget = editable.find((d) => d.staged_copies.length)?.entry.id
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
  sign && ['sign', at(`/signs/${encodeURIComponent(sign.id)}`)],
  sign && ['sign-form', at(`/signs/${encodeURIComponent(sign.id)}`), 'form'],
  sign && ['sign-refusal', at(`/signs/${encodeURIComponent(sign.id)}`), 'refusal'],
  entry && ['entry', at(`/entries/${encodeURIComponent(entry.id)}`)],
  conflict && ['entry-conflict-original', at(`/entries/${encodeURIComponent(conflict.original)}`), 'supersede'],
  conflict && ['entry-conflict-copy', at(`/entries/${encodeURIComponent(conflict.copy)}`), 'supersede'],
  editTarget && ['entry-edit', at(`/entries/${encodeURIComponent(editTarget)}`), 'edit'],
  editTarget && ['entry-edit-dark', at(`/entries/${encodeURIComponent(editTarget)}`), 'edit', 'dark'],
  editTarget && ['entry-edit-refusal', at(`/entries/${encodeURIComponent(editTarget)}`), 'edit-refusal'],
  editTarget && ['entry-edit-conflict', at(`/entries/${encodeURIComponent(editTarget)}`), 'edit-conflict'],
  copyTarget && ['entry-edit-staged-copy', at(`/entries/${encodeURIComponent(copyTarget)}`), 'edit-copy'],
  ['sessions', at('/sessions')],
  ['gaps', at('/gaps')],
  ['provisional', at('/provisional')],
  ['staged', at('/staged')],
  ['staged-light', at('/staged'), 'light'],
  ['staged-dark', at('/staged'), 'dark'],
  stagedEntry && ['entry-staged', at(`/entries/${encodeURIComponent(stagedEntry.id)}`)],
  ['tags', at('/tags')],
  ['logs', at('/logs')],
  ['project-select', at('/'), 'select'],
  ['menu', at('/'), 'menu'],
  stagedEntry && ['staged-sweep', at('/staged'), 'sweep'],
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

// Every shot is taken through here: a page wider than its window (a horizontal scrollbar on a phone) fails the proof.
const shoot = async (page, tag, fullPage = true) => {
  const { scrollWidth, innerWidth } = await page.evaluate(() => ({ scrollWidth: document.documentElement.scrollWidth, innerWidth: window.innerWidth }))
  if (scrollWidth > innerWidth) problems.push(`${tag}: the page is ${scrollWidth}px wide in a ${innerWidth}px window`)
  const file = `${out}/${tag}.png`
  await page.screenshot({ path: file, fullPage })
  written.push(file)
}

// The edit flows write entries of their own so they can be run again: a minimal MCP client, as the seed of a project would use.
let rpcId = 0
let mcpSession
const rpc = async (method, params) => {
  const r = await fetch(base + '/mcp', { method: 'POST', headers: { 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream', ...(mcpSession ? { 'Mcp-Session-Id': mcpSession } : {}) }, body: JSON.stringify({ jsonrpc: '2.0', id: ++rpcId, method, params }) })
  mcpSession = r.headers.get('mcp-session-id') || mcpSession
  const t = await r.text()
  return t.includes('data:') ? JSON.parse(t.split('\n').find((l) => l.startsWith('data:')).slice(5)) : JSON.parse(t)
}
const call = async (name, args) => {
  const j = await rpc('tools/call', { name, arguments: args })
  const t = j.result?.content?.[0]?.text ?? JSON.stringify(j)
  if (j.result?.isError || j.error) throw new Error(`${name}: ${t}`)
  return JSON.parse(t)
}
// Invented words keep a fixture from resembling anything already in the wiki, which the duplicate rule would refuse.
const syl = ['ka', 'vo', 'ri', 'zu', 'mel', 'tor', 'ban', 'qui', 'sel', 'dra', 'phe', 'nox']
const word = () => Array.from({ length: 3 }, () => syl[Math.floor(Math.random() * syl.length)]).join('') + Math.floor(Math.random() * 900 + 100)
const words = (n) => Array.from({ length: n }, word).join(' ')

for (const width of widths) {
  const ctx = await browser.newContext({ viewport: { width, height: 800 } })
  const schemed = { light: await browser.newContext({ viewport: { width, height: 800 }, colorScheme: 'light' }), dark: await browser.newContext({ viewport: { width, height: 800 }, colorScheme: 'dark' }) }
  for (const [name, path, mode, scheme] of shots) {
    if (mode === 'menu' && width > 700) continue
    const page = await (schemed[scheme ?? mode] ?? ctx).newPage()
    const tag = `${name}-${width}`
    page.on('pageerror', (e) => problems.push(`${tag}: page error: ${e.message}`))
    page.on('console', (m) => { // The refusal shot provokes a 400 on purpose; the browser logs it as a console error.
      if (m.type() === 'error' && !(/refusal|edit-refusal/.test(mode ?? '') && /status of 400/.test(m.text())) && !(mode === 'edit-conflict' && /status of 409/.test(m.text()))) problems.push(`${tag}: console error: ${m.text()}`) })
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
      if (mode?.startsWith('edit')) {
        await page.getByRole('button', { name: 'Edit', exact: true }).click()
        const form = page.getByRole('form', { name: 'Edit entry' })
        await form.waitFor({ timeout: 5000 })
        const save = form.getByRole('button', { name: 'Save', exact: true })
        const focused = (sel) => page.evaluate((q) => document.activeElement?.matches(q), sel)
        const labelless = await form.evaluate((f) => [...f.querySelectorAll('input:not([type=radio]), textarea, select')].filter((el) => !el.labels?.length && !el.getAttribute('aria-label')).length)
        if (labelless) problems.push(`${tag}: ${labelless} form field(s) without a label`)
        if (mode === 'edit-refusal') {
          await form.getByRole('group', { name: 'Anchor 1' }).getByLabel('Path').fill('src/missing.go')
          await save.click()
          await page.getByRole('alert').filter({ hasText: 'anchor' }).waitFor({ timeout: 5000 })
          if (!(await focused('.refusal[role=alert]'))) problems.push(`${tag}: focus did not move to the refusal`)
        }
        if (mode === 'edit-conflict') {
          const now = await getJSON(`${api}/entries/${encodeURIComponent(editTarget)}`)
          const r = await fetch(`${base}${api}/entries/${encodeURIComponent(editTarget)}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ etag: now.etag, title: now.entry.title, read_when: now.entry.read_when, body_markdown: now.entry.body_markdown + '\n\nAdded by someone else while the form was open.', tags: now.entry.tags, anchors: now.form_anchors, requires: now.requires, see_also: now.see_also, decided_by: now.decided_by, decided_at: now.decided_at }) })
          if (!r.ok) problems.push(`${tag}: the concurrent save failed: ${r.status} ${await r.text()}`)
          await form.getByLabel('Title').fill(now.entry.title + ' (my draft)')
          await save.click()
          await page.getByRole('heading', { name: /changed while you were editing/ }).waitFor({ timeout: 5000 })
          if (!(await focused('aside.newer'))) problems.push(`${tag}: focus did not move to the conflict view`)
          if ((await form.getByLabel('Title').inputValue()) !== now.entry.title + ' (my draft)') problems.push(`${tag}: the draft was not kept`)
        }
        if (mode === 'edit-copy' && !(await form.getByText(/holds a staged copy/).count())) problems.push(`${tag}: no staged-copy warning`)
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
      if (mode === 'sweep') {
        await page.getByRole('button', { name: 'Sweep now' }).click()
        await page.getByRole('status').waitFor()
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
      await shoot(page, tag, !!mode?.startsWith('edit'))
    } catch (e) {
      problems.push(`${tag}: ${e.message}`)
    }
    await page.close()
  }
  await ctx.close()
  await schemed.light.close()
  await schemed.dark.close()
}
// The browser edit flows. They save, so they run after every other shot and write the entries they need, which keeps them repeatable.
// A stale entry whose anchor no longer resolves cannot be made here without touching the repository, so that flow skips when the project holds none.
const flowBase = editable.find((d) => d.form_anchors.some((a) => a.type === 'file') && d.sign_id)
const staleTarget = editable.find((d) => d.entry.tier === 'stale')
// They write to the project, so they run only when asked: point PROOF_WRITES=1 at a scratch server, never at a real map.
const writes = process.env.PROOF_WRITES === '1'
if (!writes) console.log('skipped the edit flows: they write to the project; set PROOF_WRITES=1 against a scratch server')
else if (!flowBase) console.log('skipped the edit flows: no editable entry with a file anchor to borrow')
if (writes && !staleTarget) console.log('skipped the stale Confirm flow: the project holds no stale entry')
if (writes && flowBase) {
  const anchors = [flowBase.form_anchors.find((a) => a.type === 'file')]
  const session = await call('session_start', { task: 'proof: edit flows', project })
  const make = (title, read_when, body) => call('write', { session_id: session.session_id, parent_sign_id: flowBase.sign_id, kind: 'summary', title, read_when, body, anchors })
  const entryAt = (id) => at(`/entries/${encodeURIComponent(id)}`)
  const tierOnPage = (page) => page.locator('.badges .tier').first().innerText()
  const heading = (page) => page.getByRole('heading', { level: 1 }).innerText()
  const forEdit = (page) => page.getByRole('form', { name: 'Edit entry' })
  const openEdit = async (page) => {
    await page.getByRole('button', { name: 'Edit', exact: true }).click()
    await forEdit(page).waitFor({ timeout: 5000 })
  }
  const gone = (page) => forEdit(page).waitFor({ state: 'detached', timeout: 5000 })
  const expect = (tag, ok, what) => { if (!ok) problems.push(`${tag}: ${what}`) }

  for (const width of widths) {
    const ctx = await browser.newContext({ viewport: { width, height: 800 } })
    const run = async (name, fn) => {
      const page = await ctx.newPage()
      const tag = `${name}-${width}`
      const posts = []
      page.on('pageerror', (e) => problems.push(`${tag}: page error: ${e.message}`))
      // The refusals these flows provoke are logged by the browser as 4xx console errors.
      page.on('console', (m) => { if (m.type() === 'error' && !/status of 4\d\d/.test(m.text())) problems.push(`${tag}: console error: ${m.text()}`) })
      page.on('request', (r) => { if (r.method() === 'POST') posts.push(r.url()) })
      try {
        await fn(page, tag, posts)
      } catch (e) {
        problems.push(`${tag}: ${e.message}`)
      }
      await page.close()
    }

    // A duplicate refusal on an edit, then its resolution as distinct with a reason. 1280 submits with Enter in the reason field, 390 with the button.
    await run('entry-edit-duplicate', async (page, tag) => {
      const original = { title: words(3), read_when: `You are asked ${words(5)}`, body: `${words(8)}.\n\n${words(6)}.` }
      await make(original.title, original.read_when, original.body)
      const mine = await make(words(3), `You are asked ${words(5)}`, `${words(8)}.\n\n${words(6)}.`)
      await page.goto(base + entryAt(mine.entry_id), { waitUntil: 'networkidle' })
      await openEdit(page)
      const form = forEdit(page)
      await form.getByLabel('Title').fill(original.title)
      await form.getByLabel('Read when').fill(original.read_when)
      await form.getByLabel('Body (Markdown)').fill(original.body)
      await form.getByRole('button', { name: 'Save', exact: true }).click()
      const alert = page.locator('.refusal[role=alert]')
      await alert.waitFor({ timeout: 5000 })
      const text = await alert.innerText()
      expect(tag, /^duplicate/.test(text), `the refusal is not the duplicate rule: ${text}`)
      expect(tag, /replace/.test(text) && /distinct/.test(text) && !/merge/.test(text), `an edit's duplicate refusal must offer replace or distinct and not merge: ${text}`)
      const panel = form.getByRole('group', { name: /already exists/ })
      expect(tag, (await panel.count()) === 1, 'no resolution panel under the refusal')
      expect(tag, (await panel.getByRole('radio').count()) === 2, 'the panel does not offer exactly replace and keep both')
      await panel.getByRole('radio', { name: /Keep both/ }).check()
      const why = panel.getByLabel('Why it is distinct')
      await why.waitFor({ timeout: 5000 })
      await why.fill('A different question')
      await shoot(page, tag)
      if (width > 700) await why.press('Enter')
      else await panel.getByRole('button', { name: 'Save with this choice' }).click()
      await gone(page)
      await page.waitForFunction((t) => document.querySelector('h1')?.textContent === t, original.title)
      expect(tag, (await tierOnPage(page)) === 'provisional', `the tier changed on save: ${await tierOnPage(page)}`)
      const saved = await getJSON(`${api}/entries/${encodeURIComponent(mine.entry_id)}`)
      expect(tag, saved.entry.title === original.title && saved.entry.read_when === original.read_when, 'the resolved save did not store the edit')
      expect(tag, saved.superseded_by === '', 'distinct superseded the entry')
      await shoot(page, `entry-edit-duplicate-saved-${width}`)
    })

    // Confirm on the stale entry is refused and names the anchor that failed.
    if (staleTarget) {
      await run('entry-confirm-stale', async (page, tag, posts) => {
        await page.goto(base + entryAt(staleTarget.entry.id), { waitUntil: 'networkidle' })
        await page.getByRole('button', { name: 'Confirm', exact: true }).click()
        const alert = page.locator('.refusal[role=alert]')
        await alert.waitFor({ timeout: 5000 })
        const text = await alert.innerText()
        const failedAnchor = staleTarget.entry.anchors.find((a) => !a.ok)?.path_or_url
        expect(tag, !!failedAnchor && text.includes(failedAnchor), `the refusal does not name the failed anchor ${failedAnchor}: ${text}`)
        expect(tag, /^stale/.test(text), `the refusal is not the stale rule: ${text}`)
        expect(tag, await page.evaluate(() => document.activeElement?.matches('.refusal[role=alert]')), 'focus did not move to the refusal')
        expect(tag, (await tierOnPage(page)) === 'stale', 'the stale entry changed tier')
        await shoot(page, tag)
      })
    }

    // Confirm on an unchanged provisional entry verifies it; a save then shows the new text and keeps that tier.
    await run('entry-confirm-ok', async (page, tag) => {
      const body = `${words(8)}.\n\n${words(6)}.`
      const fresh = await make(words(3), `You are asked ${words(5)}`, body)
      await page.goto(base + entryAt(fresh.entry_id), { waitUntil: 'networkidle' })
      expect(tag, (await tierOnPage(page)) === 'provisional', 'the fixture did not start provisional')
      await page.getByRole('button', { name: 'Confirm', exact: true }).click()
      await page.waitForFunction(() => document.querySelector('.badges .tier')?.textContent.trim() === 'verified', null, { timeout: 5000 })
      expect(tag, (await page.locator('.refusal[role=alert]').count()) === 0, 'a refusal shows after a successful Confirm')
      expect(tag, (await getJSON(`${api}/entries/${encodeURIComponent(fresh.entry_id)}`)).entry.tier === 'verified', 'the server does not hold the entry as verified')
      await shoot(page, tag)

      await openEdit(page)
      const edited = `${words(8)}. Edited in the browser.`
      await forEdit(page).getByLabel('Body (Markdown)').fill(edited)
      await forEdit(page).getByRole('button', { name: 'Save', exact: true }).click()
      await gone(page)
      await page.waitForFunction((t) => document.body.innerText.includes(t), edited, { timeout: 5000 })
      expect(tag, (await tierOnPage(page)) === 'verified', `the tier changed on save: ${await tierOnPage(page)}`)
      expect(tag, !(await page.locator('main').innerText()).includes(body.split('\n\n')[0]), 'the old text still shows after save')
      await shoot(page, `entry-edit-saved-${width}`)
    })

    // Cancel drops the draft: nothing is sent, the page shows the stored title and a second Edit starts from it.
    await run('entry-edit-cancelled', async (page, tag, posts) => {
      const fresh = await make(words(3), `You are asked ${words(5)}`, `${words(8)}.`)
      await page.goto(base + entryAt(fresh.entry_id), { waitUntil: 'networkidle' })
      const title = await heading(page)
      await openEdit(page)
      await forEdit(page).getByLabel('Title').fill('A draft that must not survive')
      await forEdit(page).getByRole('button', { name: 'Cancel', exact: true }).click()
      await gone(page)
      expect(tag, (await heading(page)) === title, 'the title changed after Cancel')
      expect(tag, !(await page.locator('body').innerText()).includes('A draft that must not survive'), 'the draft shows after Cancel')
      expect(tag, posts.length === 0, `Cancel sent ${posts.length} POST(s)`)
      await page.evaluate(() => window.scrollTo(0, 0))
      await shoot(page, tag, false)
      await openEdit(page)
      expect(tag, (await forEdit(page).getByLabel('Title').inputValue()) === title, 'the next Edit did not start from the stored title')
    })
    await ctx.close()
  }
  await call('session_end', { session_id: session.session_id, summary: 'proof fixtures' }).catch(() => {})
}

await browser.close()

console.log(`wrote ${written.length} files:\n${written.join('\n')}`)
if (problems.length) {
  console.error(`\n${problems.length} problem(s):\n${problems.join('\n')}`)
  process.exit(1)
}
