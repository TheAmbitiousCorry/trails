<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { drag } from 'd3-drag'
import { forceCollide, forceLink, forceManyBody, forceSimulation, forceX, forceY, type Simulation, type SimulationLinkDatum, type SimulationNodeDatum } from 'd3-force'
import { select, type Selection } from 'd3-selection'
import { zoom, zoomIdentity, type ZoomBehavior } from 'd3-zoom'
import { hopVerb, useApi, type GraphEdge, type GraphHop, type GraphNode, type GraphResponse } from '../api'
import { useGraph } from '../graph'
import Load from '../components/Load.vue'

interface SimNode extends SimulationNodeDatum, GraphNode { r: number; label: string; labelAlways: boolean }
type SimLink = SimulationLinkDatum<SimNode> & { relation: string }

const FOLD_ABOVE = 150
const LABEL_ENTRIES_BELOW = 60
const FIT_PAD = 24
const ROUTE_FIT_PAD = 48
const LINK_COLOURS = new Set(['requires', 'see_also', 'supersedes', 'conflicts_with'])

const route = useRoute()
const router = useRouter()
const global = useGraph()

const one = (v: unknown) => (typeof v === 'string' ? v : '')
const focus = computed(() => one(route.query.focus))
const depth = computed(() => (one(route.query.depth) === '2' ? 2 : 1))
const tag = computed(() => one(route.query.tag))
const session = computed(() => one(route.query.session))
const filtered = computed(() => !!(focus.value || tag.value || session.value))

const api = useApi<GraphResponse>(() => {
  if (!filtered.value) return null
  const q = new URLSearchParams()
  if (focus.value) q.set('focus', focus.value)
  if (focus.value) q.set('depth', String(depth.value))
  if (tag.value) q.set('tag', tag.value)
  if (session.value) q.set('session', session.value)
  return `/api/graph?${q}`
})

const graph = computed<GraphResponse | null>(() => {
  if (filtered.value) return api.data.value
  return global.nodes.value.length ? { nodes: global.nodes.value, edges: global.edges.value, route: null } : null
})
const loading = computed(() => (filtered.value ? api.loading.value : global.loading.value))
const error = computed(() => (filtered.value ? api.error.value : global.error.value))

// The tag list comes from the unfiltered graph so a filtered view can still switch tags.
const tags = computed(() => [...new Set(global.nodes.value.flatMap((n) => n.tags ?? []))].sort())
const titles = computed(() => new Map((graph.value?.nodes ?? []).map((n) => [n.id, n.title])))

// A session overlay needs every route node on screen, so it never folds.
const folded = computed(() => !!graph.value && !focus.value && !session.value && graph.value.nodes.length > FOLD_ABOVE)

function setQuery(key: string, value: string) {
  const q = { ...route.query }
  if (value) q[key] = value
  else delete q[key]
  void router.replace({ query: q })
}

const svgEl = ref<SVGSVGElement | null>(null)
let sim: Simulation<SimNode, SimLink> | null = null
// d3-zoom reads its transform from the element, so every fit goes through this one behaviour.
let zoomer: ZoomBehavior<SVGSVGElement, unknown> | null = null

function stop() {
  sim?.stop()
  sim = null
  zoomer = null
  if (svgEl.value) select(svgEl.value).selectAll('*').remove()
}

function draw() {
  stop()
  const g = graph.value
  const el = svgEl.value
  if (!g || !el) return

  const signs = g.nodes.filter((n) => n.type === 'sign')
  const subtree = new Map<string, number>()
  if (folded.value) {
    const parent = new Map(signs.map((s) => [s.id, s.parent ?? '']))
    for (const n of g.nodes) {
      if (n.type !== 'entry') continue
      for (let s = n.sign_id ?? ''; s; s = parent.get(s) ?? '') subtree.set(s, (subtree.get(s) ?? 0) + 1)
    }
  }
  const shown = folded.value ? signs : g.nodes
  const entryCount = shown.filter((n) => n.type === 'entry').length
  const labelEntries = entryCount < LABEL_ENTRIES_BELOW
  const maxReads = Math.max(1, ...shown.map((n) => n.reads_30d ?? 0))

  const routeIds = new Set((g.route ?? []).filter((h) => h.kind !== 'query').flatMap((h) => [h.target, ...h.prerequisites]))
  const nodes: SimNode[] = shown.map((n) => ({
    ...n,
    r: n.type === 'sign' ? 11 : 5 + 9 * Math.sqrt((n.reads_30d ?? 0) / maxReads),
    label: n.type === 'sign' && folded.value ? `${n.title} (${subtree.get(n.id) ?? 0})` : n.title,
    labelAlways: n.type === 'sign' || labelEntries || routeIds.has(n.id),
  }))
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const edges: GraphEdge[] = g.edges.filter((e) => byId.has(e.from) && byId.has(e.to))
  const links: SimLink[] = edges.map((e) => ({ source: e.from, target: e.to, relation: e.relation }))

  const w = el.clientWidth || 800
  const h = el.clientHeight || 600
  const svg = select(el).attr('viewBox', `0 0 ${w} ${h}`)

  const relations = [...new Set(edges.map((e) => e.relation).filter((r) => LINK_COLOURS.has(r)))]
  svg.append('defs').selectAll('marker').data(relations).join('marker')
    .attr('id', (r) => `arrow-${r}`).attr('class', (r) => `g-marker g-rel-${r}`)
    .attr('viewBox', '0 0 10 10').attr('refX', 10).attr('refY', 5).attr('markerWidth', 7).attr('markerHeight', 7)
    .attr('orient', 'auto-start-reverse')
    .append('path').attr('d', 'M0,0 L10,5 L0,10 z')

  svg.select('defs').append('marker').attr('id', 'arrow-route').attr('class', 'g-route-marker')
    .attr('viewBox', '0 0 10 10').attr('refX', 9).attr('refY', 5).attr('markerUnits', 'userSpaceOnUse').attr('markerWidth', 12).attr('markerHeight', 12)
    .attr('orient', 'auto')
    .append('path').attr('d', 'M0,0 L10,5 L0,10 z')

  // Once the reader pans, zooms or drags, the automatic fits stop so they do not undo that.
  let userMoved = false
  const layer = svg.append('g')
  zoomer = zoom<SVGSVGElement, unknown>().scaleExtent([0.2, 6]).on('zoom', (ev) => {
    layer.attr('transform', ev.transform.toString())
    if (ev.sourceEvent) userMoved = true
  })
  svg.call(zoomer)

  const linkSel = layer.append('g').selectAll('line').data(links).join('line')
    .attr('class', (l) => `g-edge ${LINK_COLOURS.has(l.relation) ? `g-rel-${l.relation}` : 'g-home'}`)
    .attr('marker-end', (l) => (LINK_COLOURS.has(l.relation) ? `url(#arrow-${l.relation})` : null))

  const nodeSel = layer.append('g').selectAll<SVGGElement, SimNode>('g').data(nodes).join('g')
    .attr('class', 'g-node').attr('tabindex', 0).attr('role', 'link')
    .attr('aria-label', (n) => `${n.type} ${n.title}`)
  nodeSel.each(function (n) {
    const s = select(this)
    if (n.type === 'sign') s.append('rect').attr('class', 'g-sign').attr('x', -n.r).attr('y', -n.r).attr('width', n.r * 2).attr('height', n.r * 2).attr('rx', 4)
    else s.append('circle').attr('class', `g-entry g-tier-${n.tier}`).attr('r', n.r)
    s.append('title').text(n.title)
    s.append('text').attr('class', n.labelAlways ? 'g-label' : 'g-label g-label-hover').attr('x', n.r + 4).attr('y', 4).text(n.label)
  })

  const open = (n: SimNode) => void router.push(n.type === 'sign' ? { name: 'sign', params: { id: n.id } } : { name: 'entry', params: { id: n.id } })
  nodeSel.on('click', (_, n) => open(n)).on('keydown', (ev: KeyboardEvent, n) => { if (ev.key === 'Enter') open(n) })

  const linked = (n: SimNode, l: SimLink) => (l.source as SimNode) === n || (l.target as SimNode) === n
  nodeSel.on('mouseenter focus', (_, n) => {
    linkSel.classed('g-hot', (l) => linked(n, l)).classed('g-dim', (l) => !linked(n, l))
  }).on('mouseleave blur', () => linkSel.classed('g-hot', false).classed('g-dim', false))

  const small = nodes.length < 40
  const homeDist = small ? 90 : 40
  const typedDist = small ? 90 : 70
  sim = forceSimulation(nodes)
    .force('link', forceLink<SimNode, SimLink>(links).id((n) => n.id).distance((l) => (l.relation === 'home' ? homeDist : typedDist)))
    .force('charge', forceManyBody().strength(small ? -300 : -120))
    .force('x', forceX<SimNode>(w / 2).strength(0.05))
    .force('y', forceY<SimNode>(h / 2).strength(0.05))
    .force('collide', forceCollide<SimNode>((n) => n.r + 4))
    .alphaDecay(0.04)
    .stop()
  // Centre-based collision cannot keep side labels apart, so small graphs also push overlapping label boxes apart vertically.
  if (small) sim.force('labels', labelForce(nodes))

  nodeSel.call(drag<SVGGElement, SimNode>()
    .on('start', (ev, n) => { if (!ev.active) sim?.alphaTarget(0.3).restart(); n.fx = n.x; n.fy = n.y; userMoved = true })
    .on('drag', (ev, n) => { n.fx = ev.x; n.fy = ev.y })
    .on('end', (ev, n) => { if (!ev.active) sim?.alphaTarget(0); n.fx = null; n.fy = null }))

  const overlay = drawRoute(layer, byId, nodeSel)
  const paint = () => {
    linkSel.attr('x1', (l) => (l.source as SimNode).x ?? 0).attr('y1', (l) => (l.source as SimNode).y ?? 0)
      .attr('x2', (l) => (l.target as SimNode).x ?? 0).attr('y2', (l) => (l.target as SimNode).y ?? 0)
    nodeSel.attr('transform', (n) => `translate(${n.x ?? 0},${n.y ?? 0})`)
    overlay()
  }

  // With a session set the view frames the walk, not the whole graph.
  const fit = () => {
    if (userMoved || !zoomer) return
    const onRoute = routeIds.size ? nodes.filter((n) => routeIds.has(n.id)) : []
    const box = onRoute.length ? onRoute : nodes
    const pad = onRoute.length ? ROUTE_FIT_PAD : FIT_PAD
    let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity
    for (const n of box) {
      const x = n.x ?? 0, y = n.y ?? 0
      // Visible labels sit to the right of the node, so they count towards the box.
      const lw = labelWidth(n)
      x0 = Math.min(x0, x - n.r); y0 = Math.min(y0, y - n.r)
      x1 = Math.max(x1, x + n.r + lw); y1 = Math.max(y1, y + n.r)
    }
    const cw = el.clientWidth || w
    const ch = el.clientHeight || h
    const k = Math.min(onRoute.length ? 1.5 : 1, Math.max(0.2, Math.min((cw - 2 * pad) / Math.max(x1 - x0, 1), (ch - 2 * pad) / Math.max(y1 - y0, 1))))
    const t = zoomIdentity.translate(cw / 2 - k * (x0 + x1) / 2, ch / 2 - k * (y0 + y1) / 2).scale(k)
    svg.call(zoomer.transform, t)
  }

  // Settle most of the layout before the first paint so the opening frame is already framed.
  sim.tick(100)
  paint()
  fit()
  let ticks = 100
  let fallback = false
  sim.on('tick', () => {
    paint()
    if (++ticks === 300 && !fallback) { fallback = true; fit() }
  })
  sim.on('end', () => { fallback = true; fit() })
  sim.restart()
}

function labelWidth(n: SimNode): number {
  return n.labelAlways ? Math.min(n.label.length * 6.2, 300) + 4 : 0
}

function labelForce(nodes: SimNode[]) {
  return () => {
    for (let i = 0; i < nodes.length; i++) {
      for (let j = i + 1; j < nodes.length; j++) {
        const a = nodes[i], b = nodes[j]
        const ax = a.x ?? 0, bx = b.x ?? 0, dy = (b.y ?? 0) - (a.y ?? 0)
        const ox = Math.min(ax + a.r + labelWidth(a), bx + b.r + labelWidth(b)) - Math.max(ax - a.r, bx - b.r)
        const oy = (Math.max(2 * a.r, 14) + Math.max(2 * b.r, 14)) / 2 + 2 - Math.abs(dy)
        if (ox <= 0 || oy <= 0) continue
        const push = oy * 0.15 * (dy >= 0 ? 1 : -1)
        a.vy = (a.vy ?? 0) - push
        b.vy = (b.vy ?? 0) + push
      }
    }
  }
}

type Layer = Selection<SVGGElement, unknown, null, undefined>

// Returns a function that re-positions the route overlay; query hops have no node and are skipped.
function drawRoute(layer: Layer, byId: Map<string, SimNode>, nodeSel: Selection<SVGGElement, SimNode, SVGGElement, unknown>): () => void {
  const hops = (graph.value?.route ?? []).filter((h) => h.kind !== 'query' && byId.has(h.target))
  if (!hops.length) return () => {}
  const onRoute = new Set(hops.map((h) => h.target))
  nodeSel.classed('g-on-route', (n) => onRoute.has(n.id))
  const g = layer.append('g').attr('class', 'g-route')
  const prereq = hops.flatMap((h) => h.prerequisites.filter((p) => byId.has(p) && p !== h.target).map((p) => ({ from: p, to: h.target })))
  const dashed = g.append('g').selectAll('line').data(prereq).join('line').attr('class', 'g-prereq')
  // Consecutive hops on one node have nothing to join, so they get no segment.
  const steps = hops.slice(1).map((h, i) => ({ from: hops[i].target, to: h.target })).filter((s) => s.from !== s.to)
  const segs = g.append('g').selectAll('path').data(steps).join('path').attr('class', 'g-path').attr('marker-end', 'url(#arrow-route)')
  const seen = new Map<string, number>()
  const marks = hops.map((h) => {
    const k = seen.get(h.target) ?? 0
    seen.set(h.target, k + 1)
    return { h, k }
  })
  const badge = g.append('g').selectAll('g').data(marks).join('g').attr('class', 'g-hop')
  badge.append('circle').attr('r', 8)
  badge.append('text').attr('y', 3.5).text((m) => m.h.n)
  const pos = (id: string) => byId.get(id)!
  return () => {
    // Segments stop at the target's rim so the arrowhead is not hidden under the node.
    segs.attr('d', (s) => {
      const a = pos(s.from), b = pos(s.to)
      const ax = a.x ?? 0, ay = a.y ?? 0, bx = b.x ?? 0, by = b.y ?? 0
      const len = Math.hypot(bx - ax, by - ay) || 1
      const ux = (bx - ax) / len, uy = (by - ay) / len
      return `M${ax + ux * a.r},${ay + uy * a.r}L${bx - ux * (b.r + 2)},${by - uy * (b.r + 2)}`
    })
    dashed.attr('x1', (d) => pos(d.from).x ?? 0).attr('y1', (d) => pos(d.from).y ?? 0).attr('x2', (d) => pos(d.to).x ?? 0).attr('y2', (d) => pos(d.to).y ?? 0)
    badge.attr('transform', (m) => {
      const n = pos(m.h.target)
      return `translate(${(n.x ?? 0) + n.r * 0.7 + m.k * 18},${(n.y ?? 0) - n.r * 0.7})`
    })
  }
}

const hops = computed<GraphHop[]>(() => graph.value?.route ?? [])

onMounted(draw)
watch([graph, folded], draw, { flush: 'post' })
onBeforeUnmount(stop)
</script>

<template>
  <h1>Graph</h1>
  <p v-if="session && graph?.session" class="muted">Route of session: {{ graph.session.task }}</p>
  <div class="controls">
    <div v-if="focus" class="depth-toggle" role="group" aria-label="Depth">
      <span class="muted small">Depth</span>
      <button type="button" :aria-pressed="depth === 1" @click="setQuery('depth', '1')">1</button>
      <button type="button" :aria-pressed="depth === 2" @click="setQuery('depth', '2')">2</button>
    </div>
    <label>Tag
      <select :value="tag" @change="setQuery('tag', ($event.target as HTMLSelectElement).value)">
        <option value="">All entries</option>
        <option v-for="t in tags" :key="t" :value="t">{{ t }}</option>
      </select>
    </label>
    <RouterLink v-if="focus" to="/graph" class="whole-map">Whole map</RouterLink>
  </div>
  <p class="legend" aria-label="Legend">
    <span class="lg"><svg width="14" height="14" aria-hidden="true"><rect class="g-sign" x="1" y="1" width="12" height="12" rx="3" /></svg>sign</span>
    <span class="lg"><svg width="14" height="14" aria-hidden="true"><circle class="g-entry g-tier-verified" cx="7" cy="7" r="6" /></svg>verified</span>
    <span class="lg"><svg width="14" height="14" aria-hidden="true"><circle class="g-entry g-tier-provisional" cx="7" cy="7" r="6" /></svg>provisional</span>
    <span class="lg"><svg width="14" height="14" aria-hidden="true"><circle class="g-entry g-tier-stale" cx="7" cy="7" r="6" /></svg>stale</span>
    <span class="lg"><svg width="22" height="10" aria-hidden="true"><line class="g-edge g-home" x1="0" y1="5" x2="22" y2="5" /></svg>home</span>
    <span v-for="r in ['requires', 'see_also', 'supersedes', 'conflicts_with']" :key="r" class="lg">
      <svg width="22" height="10" aria-hidden="true"><line class="g-edge" :class="`g-rel-${r}`" x1="0" y1="5" x2="22" y2="5" /></svg>{{ r.replace('_', ' ') }}
    </span>
  </p>
  <p v-if="folded" class="muted small">More than {{ FOLD_ABOVE }} nodes: showing signs only, each labelled with its entry count. Focus a sign to see its entries.</p>
  <Load :ready="!!graph" :loading="loading" :error="error">
    <div class="graph-layout" :class="{ 'has-route': hops.length }">
      <svg ref="svgEl" class="graph-svg" role="img" aria-label="Graph of signs and entries"></svg>
      <aside v-if="session && graph?.route" aria-labelledby="hops-h" class="hop-list">
        <h2 id="hops-h">Route</h2>
        <p v-if="!hops.length" class="muted small">This session made no calls after it started.</p>
        <ol class="plain">
          <li v-for="h in hops" :key="h.n">
            <span class="muted small">{{ h.n }}.</span>
            {{ hopVerb(h.tool) }}
            <RouterLink v-if="h.kind === 'sign'" :to="{ name: 'sign', params: { id: h.target } }">{{ titles.get(h.target) ?? h.target }}</RouterLink>
            <RouterLink v-else-if="h.kind === 'entry'" :to="{ name: 'entry', params: { id: h.target } }">{{ titles.get(h.target) ?? h.target }}</RouterLink>
            <q v-else-if="h.target">{{ h.target }}</q>
            <span class="badges">
              <span v-if="h.turn_back" class="pill pill-turn">turned back</span>
              <span v-if="h.miss" class="pill pill-stale">miss</span>
              <span v-if="h.result.startsWith('refused')" class="pill pill-stale">{{ h.result }}</span>
            </span>
            <span v-if="h.prerequisites.length" class="small">
              Chain: also served
              <template v-for="(p, i) in h.prerequisites" :key="p"><template v-if="i">, </template><RouterLink :to="{ name: 'entry', params: { id: p } }">{{ titles.get(p) ?? p }}</RouterLink></template>
            </span>
          </li>
        </ol>
      </aside>
    </div>
  </Load>
</template>
