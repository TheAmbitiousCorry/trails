<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { drag } from 'd3-drag'
import { forceCenter, forceCollide, forceLink, forceManyBody, forceSimulation, type Simulation, type SimulationLinkDatum, type SimulationNodeDatum } from 'd3-force'
import { select, type Selection } from 'd3-selection'
import { zoom } from 'd3-zoom'
import { hopVerb, useApi, type GraphEdge, type GraphHop, type GraphNode, type GraphResponse } from '../api'
import { useGraph } from '../graph'
import Load from '../components/Load.vue'

interface SimNode extends SimulationNodeDatum, GraphNode { r: number; label: string; labelAlways: boolean }
type SimLink = SimulationLinkDatum<SimNode> & { relation: string }

const FOLD_ABOVE = 150
const LABEL_ENTRIES_BELOW = 60
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

function stop() {
  sim?.stop()
  sim = null
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

  const nodes: SimNode[] = shown.map((n) => ({
    ...n,
    r: n.type === 'sign' ? 11 : 5 + 9 * Math.sqrt((n.reads_30d ?? 0) / maxReads),
    label: n.type === 'sign' && folded.value ? `${n.title} (${subtree.get(n.id) ?? 0})` : n.title,
    labelAlways: n.type === 'sign' || labelEntries,
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

  const layer = svg.append('g')
  svg.call(zoom<SVGSVGElement, unknown>().scaleExtent([0.2, 6]).on('zoom', (ev) => layer.attr('transform', ev.transform.toString())))

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

  sim = forceSimulation(nodes)
    .force('link', forceLink<SimNode, SimLink>(links).id((n) => n.id).distance((l) => (l.relation === 'home' ? 45 : 90)))
    .force('charge', forceManyBody().strength(-160))
    .force('center', forceCenter(w / 2, h / 2))
    .force('collide', forceCollide<SimNode>((n) => n.r + 4))

  nodeSel.call(drag<SVGGElement, SimNode>()
    .on('start', (ev, n) => { if (!ev.active) sim?.alphaTarget(0.3).restart(); n.fx = n.x; n.fy = n.y })
    .on('drag', (ev, n) => { n.fx = ev.x; n.fy = ev.y })
    .on('end', (ev, n) => { if (!ev.active) sim?.alphaTarget(0); n.fx = null; n.fy = null }))

  const overlay = drawRoute(layer, byId)
  sim.on('tick', () => {
    linkSel.attr('x1', (l) => (l.source as SimNode).x ?? 0).attr('y1', (l) => (l.source as SimNode).y ?? 0)
      .attr('x2', (l) => (l.target as SimNode).x ?? 0).attr('y2', (l) => (l.target as SimNode).y ?? 0)
    nodeSel.attr('transform', (n) => `translate(${n.x ?? 0},${n.y ?? 0})`)
    overlay()
  })
}

type Layer = Selection<SVGGElement, unknown, null, undefined>

// Returns a function that re-positions the route overlay; query hops have no node and are skipped.
function drawRoute(layer: Layer, byId: Map<string, SimNode>): () => void {
  const hops = (graph.value?.route ?? []).filter((h) => h.kind !== 'query' && byId.has(h.target))
  if (!hops.length) return () => {}
  const g = layer.append('g').attr('class', 'g-route')
  const path = g.append('path').attr('class', 'g-path')
  const prereq = hops.flatMap((h) => h.prerequisites.filter((p) => byId.has(p)).map((p) => ({ from: p, to: h.target })))
  const dashed = g.selectAll('line').data(prereq).join('line').attr('class', 'g-prereq')
  const seen = new Map<string, number>()
  const marks = hops.map((h) => {
    const k = seen.get(h.target) ?? 0
    seen.set(h.target, k + 1)
    return { h, k }
  })
  const badge = g.selectAll('g').data(marks).join('g').attr('class', 'g-hop')
  badge.append('circle').attr('r', 8)
  badge.append('text').attr('y', 3.5).text((m) => m.h.n)
  const pos = (id: string) => byId.get(id)!
  return () => {
    path.attr('d', hops.map((h, i) => `${i ? 'L' : 'M'}${pos(h.target).x ?? 0},${pos(h.target).y ?? 0}`).join(''))
    dashed.attr('x1', (d) => pos(d.from).x ?? 0).attr('y1', (d) => pos(d.from).y ?? 0).attr('x2', (d) => pos(d.to).x ?? 0).attr('y2', (d) => pos(d.to).y ?? 0)
    badge.attr('transform', (m) => {
      const n = pos(m.h.target)
      return `translate(${(n.x ?? 0) + n.r + 4 + m.k * 18},${(n.y ?? 0) - n.r - 4})`
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
