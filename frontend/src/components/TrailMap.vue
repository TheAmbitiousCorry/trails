<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { easeLinear } from 'd3-ease'
import { select, type Selection } from 'd3-selection'
import { zoom, zoomIdentity, type ZoomBehavior } from 'd3-zoom'
import 'd3-transition'
import type { GraphEdge, GraphNode } from '../api'
import { useGraph } from '../graph'
import { place, subtree, type LayoutKind, type Placed, type Placement } from '../mapLayout'
import { buildTree, foldGroups } from '../mapmodel'

/** `target` empty or off the map draws nothing; `label` is the badge text, the hop's position when omitted. */
export interface ReplayHop { target: string; prerequisites?: string[]; label?: number }

const props = defineProps<{
  layout: LayoutKind
  /** Sign id whose subtree is drawn; the whole map when empty. */
  root?: string
  highlight?: string[]
  /** Hops to draw, each walked from the drawn root to its target. */
  replay?: ReplayHop[]
  /** Hops drawn instantly, 0 to replay.length; leave unset to draw nothing until play() runs. */
  at?: number
  /** With `at`, a hop newly reached (at one more than before) is walked with motion instead of drawn at once. */
  live?: boolean
  /** Replaces the shared graph, for a server-filtered copy. */
  data?: { nodes: GraphNode[]; edges: GraphEdge[] }
}>()
const emit = defineEmits<{ open: [id: string] }>()

const HOP_MS = 650
const FIT_PAD = 24
const GRID_STEP = 80
// Fitting never magnifies, so a small subtree keeps the label size of a large one.
const FIT_MAX = 1

const shared = useGraph()
const nodes = computed(() => props.data?.nodes ?? shared.nodes.value)
const edges = computed(() => props.data?.edges ?? shared.edges.value)
const typedEdges = computed(() => edges.value.filter((e) => e.relation !== 'home'))
const placement = computed<Placement | null>(() => {
  const whole = buildTree(nodes.value, edges.value)
  if (!whole) return null
  const sub = subtree(whole, props.root ?? '')
  return place(props.layout, props.layout === 'cluster' ? sub : foldGroups(sub))
})

const host = ref<HTMLDivElement>()
const svgEl = ref<SVGSVGElement>()
let svg: Selection<SVGSVGElement, unknown, null, undefined>
let canvas: Selection<SVGGElement, unknown, null, undefined>
let replayLayer: Selection<SVGGElement, unknown, null, undefined>
let behaviour: ZoomBehavior<SVGSVGElement, unknown>
let moved = false
let resizer: ResizeObserver | undefined

const reduceMotion = () => matchMedia('(prefers-reduced-motion: reduce)').matches
const trailColour = (p: Placed) => (p.trail < 0 ? 'var(--fg)' : `var(--trail-${(p.trail % 8) + 1})`)
const trailVar = (i: number) => `var(--trail-${(i % 8) + 1})`
const tierColour = (t?: string) => (t === 'verified' ? 'var(--verified)' : t === 'stale' ? 'var(--stale)' : 'var(--provisional)')
const nodeSize = (p: Placed) => 4 + Math.sqrt(p.node.data.reads)

/** Road width by the kind of road: trailhead highways, then roads to a sub-sign or domain, then side streets to entries. */
const roadWidth = (l: { source: { depth: number }; target: { children?: unknown } }) => (l.source.depth === 0 ? 8 : l.target.children ? 5 : 2.8)

function draw() {
  const pl = placement.value
  canvas.selectAll('*').remove()
  replayLayer = canvas.append('g').attr('class', 'tm-replay')
  if (!pl) return
  // A highlighted entry that is folded into a group lights the group.
  const lit = new Set((props.highlight ?? []).map((id) => pl.byId.get(id)?.id ?? id))
  const dimmed = lit.size > 0
  const topo = pl.kind === 'topo'
  const cluster = pl.kind === 'cluster'
  const links = pl.root.links()
  const at = (id: string) => pl.byId.get(id)!
  const typed = typedEdges.value
    .filter((e) => pl.byId.has(e.from) && pl.byId.has(e.to))
    .map((e) => ({ a: at(e.from), b: at(e.to) }))
    .filter((l) => l.a !== l.b)
  const road = (l: { source: { data: { id: string } }; target: { data: { id: string } } }) => pl.link(at(l.source.data.id), at(l.target.data.id))

  const back = canvas.insert('g', '.tm-replay').attr('class', 'tm-back')
  if (topo) {
    const grid = back.append('g').attr('class', 'tm-grid')
    for (let v = -pl.grid; v <= pl.grid; v += GRID_STEP) {
      grid.append('line').attr('x1', v).attr('x2', v).attr('y1', -pl.grid).attr('y2', pl.grid)
      grid.append('line').attr('y1', v).attr('y2', v).attr('x1', -pl.grid).attr('x2', pl.grid)
    }
    back.append('g').attr('class', 'tm-rings').selectAll('circle').data(pl.rings.slice(1)).join('circle').attr('r', (r) => r)
    back.append('g').attr('class', 'tm-casing').selectAll('path').data(links).join('path')
      .attr('fill', 'none').attr('stroke-width', (l) => roadWidth(l) + 3).attr('d', road)
    back.append('g').attr('class', 'tm-roads').selectAll('path').data(links).join('path')
      .attr('fill', 'none').attr('stroke-width', roadWidth)
      .attr('stroke', (l) => (l.source.depth === 0 ? 'var(--bg)' : trailColour(at(l.target.data.id))))
      .attr('d', road)
  } else if (cluster) {
    back.append('g').attr('class', 'tm-hulls').selectAll('path').data(pl.hulls.filter((h) => !h.peninsula)).join('path')
      .attr('d', hullPath).attr('fill', (h) => trailVar(h.trail)).attr('stroke', (h) => trailVar(h.trail))
    back.append('g').attr('class', 'tm-peninsulas').selectAll('path').data(pl.hulls.filter((h) => h.peninsula)).join('path')
      .attr('d', hullPath).attr('fill', (h) => trailVar(h.trail)).attr('stroke', (h) => trailVar(h.trail))
    back.append('g').attr('class', 'tm-typed').selectAll('path').data(typed).join('path')
      .attr('fill', 'none').attr('d', (l) => pl.shortcut(l.a, l.b))
    back.append('g').attr('class', 'tm-spokes').selectAll('path').data(links).join('path')
      .attr('fill', 'none').attr('stroke-linecap', 'round')
      .attr('stroke', (l) => (l.source.depth === 0 ? 'var(--line)' : trailColour(at(l.target.data.id))))
      .attr('stroke-opacity', (l) => (l.source.depth === 0 ? 1 : 0.7)).attr('stroke-width', (l) => (l.source.depth === 0 ? 2 : 1.6))
      .attr('d', road)
  } else {
    back.append('g').attr('class', 'tm-links').selectAll('path').data(links).join('path')
      .attr('fill', 'none')
      .attr('stroke', (l) => trailColour(at(l.target.data.id)))
      .attr('d', road)
  }
  // Ridge and Topo draw a node's typed links only while it is hovered or focused; Cluster draws them all faint and lights the node's own.
  const hover = back.append('g').attr('class', 'tm-hover')
  const typedLines = back.selectAll<SVGPathElement, (typeof typed)[number]>('.tm-typed path')

  const g = back.append('g').selectAll<SVGGElement, Placed>('g').data(pl.placed).join('g')
    .attr('class', (p) => `tm-node${lit.has(p.id) ? ' lit' : dimmed ? ' dim' : ''}`)
    .attr('transform', (p) => `translate(${p.x},${p.y})`)
    .attr('tabindex', 0).attr('role', 'link').attr('aria-label', (p) => `${p.node.data.type} ${p.node.data.title}`)
    .on('click', (_, p) => open(p))
    .on('keydown', (ev: KeyboardEvent, p) => { if (ev.key === 'Enter') open(p) })
    .on('mouseenter focus', (_, p) => {
      const mine = typed.filter((l) => l.a.id === p.id || l.b.id === p.id)
      if (cluster) {
        const own = new Set(mine)
        typedLines.attr('stroke-opacity', (l) => (own.has(l) ? 0.9 : 0.06)).attr('stroke', (l) => (own.has(l) ? 'var(--accent)' : 'var(--mute)'))
        return
      }
      hover.selectAll('path').data(mine).join('path').attr('fill', 'none').attr('d', (l) => pl.shortcut(l.a, l.b))
    })
    .on('mouseleave blur', () => {
      hover.selectAll('*').remove()
      typedLines.attr('stroke-opacity', null).attr('stroke', null)
    })
  g.append('title').text((p) => p.node.data.title)
  const sign = (p: Placed) => p.node.data.type === 'sign'
  const entry = (p: Placed) => p.node.data.type === 'entry'
  const trailhead = (p: Placed) => p.node === pl.root && p.id === 'root'
  g.filter((p) => p.node.data.type === 'group').append('circle').attr('r', 9).attr('class', 'tm-group')
  if (topo) {
    g.filter((p) => p.node === pl.root).append('circle').attr('r', 13).attr('class', 'tm-trailhead')
    g.filter((p) => sign(p) && p.node !== pl.root).append('rect')
      .attr('x', -7).attr('y', -7).attr('width', 14).attr('height', 14).attr('rx', 3).attr('class', 'tm-road-sign').attr('stroke', trailColour)
    g.filter(entry).append('circle')
      .attr('r', (p) => 4.5 + Math.sqrt(p.node.data.reads)).attr('class', 'tm-road-entry').attr('stroke', (p) => tierColour(p.node.data.tier))
  } else if (cluster) {
    g.filter(trailhead).append('circle').attr('r', 12).attr('class', 'tm-trailhead')
    g.filter((p) => sign(p) && !trailhead(p)).append('rect')
      .attr('x', -8).attr('y', -8).attr('width', 16).attr('height', 16).attr('rx', 3).attr('fill', trailColour)
    g.filter(entry).append('circle')
      .attr('r', (p) => 4 + Math.sqrt(p.node.data.reads) * 1.6).attr('fill', (p) => tierColour(p.node.data.tier))
  } else {
    g.filter(sign).append('rect')
      .attr('x', -6).attr('y', -6).attr('width', 12).attr('height', 12).attr('rx', 2).attr('fill', trailColour)
    g.filter(entry).append('circle')
      .attr('r', nodeSize).attr('fill', (p) => tierColour(p.node.data.tier))
  }
  g.append('text')
    .attr('class', (p) => `tm-label${sign(p) ? ' sign' : ''}`)
    .text((p) => p.label)
    .each(function (p) {
      const t = select(this)
      const internal = !!p.node.children
      if (topo) {
        const flip = p.angle >= Math.PI
        const deg = (p.angle * 180) / Math.PI - 90 + (flip ? 180 : 0)
        t.attr('dy', '.32em').attr('x', flip ? -12 : 12).attr('text-anchor', flip ? 'end' : 'start').attr('transform', `rotate(${deg})`)
          .style('font-size', p.node.parent ? (internal ? '12.5px' : '11px') : '14px')
      } else if (cluster) {
        t.attr('dy', internal ? '-1.1em' : '.32em').attr('x', internal ? 0 : 10).attr('text-anchor', internal ? 'middle' : 'start')
      } else {
        t.attr('dy', internal ? '-0.9em' : '.32em').attr('x', internal ? 0 : 11 + (entry(p) ? nodeSize(p) - 4 : 0)).attr('text-anchor', internal ? 'middle' : 'start')
      }
    })
}

/** A group opens its sign, which lists the folded entries. */
function open(p: Placed) {
  if (p.node.data.type === 'group') emit('open', p.node.data.signId ?? '')
  else if (p.id !== 'root') emit('open', p.id)
}

const hullPath = (h: { points: [number, number][] }) => `M${h.points.map((q) => q.join(',')).join('L')}Z`

function hopChain(pl: Placement, target: string): Placed[] {
  const p = pl.byId.get(target)
  return p ? p.node.ancestors().reverse().map((d) => pl.byId.get(d.data.id)!) : []
}

/** Draws hops [0, count) from the drawn root; hops from `first` on are walked with motion, HOP_MS apart. */
function drawHops(count: number, first: number) {
  replayLayer.selectAll('*').remove()
  const pl = placement.value
  const hops = props.replay ?? []
  if (!pl || count <= 0) return
  const motion = !reduceMotion()
  const root = pl.byId.get(pl.root.data.id)!
  replayLayer.append('circle').attr('class', 'tm-start').attr('cx', root.x).attr('cy', root.y).attr('r', 11)
  hops.slice(0, count).forEach((hop, i) => {
    const chain = hopChain(pl, hop.target)
    if (chain.length === 0) return
    const live = motion && i >= first
    const delay = live ? (i - first) * HOP_MS : 0
    const seg = chain.length - 1
    for (let k = 0; k < seg; k++) {
      const at = delay + (k * HOP_MS) / Math.max(seg, 1)
      const dur = HOP_MS / Math.max(seg, 1)
      // The forks passed on the way: every sibling prong faint, the chosen one solid on top.
      for (const c of chain[k].node.children ?? []) {
        if (c === chain[k + 1].node) continue
        const faint = replayLayer.append('path').attr('class', 'tm-fork').attr('d', pl.link(chain[k], pl.byId.get(c.data.id)!))
        if (live) faint.attr('stroke-opacity', 0).transition().delay(at).duration(150).attr('stroke-opacity', 0.14)
      }
      const solid = replayLayer.append('path').attr('class', 'tm-walk').attr('d', pl.link(chain[k], chain[k + 1]))
      if (live) {
        const len = (solid.node() as SVGPathElement).getTotalLength()
        solid.attr('stroke-dasharray', len).attr('stroke-dashoffset', len)
          .transition().delay(at).duration(dur).ease(easeLinear).attr('stroke-dashoffset', 0)
      }
    }
    const target = chain[chain.length - 1]
    for (const id of hop.prerequisites ?? []) {
      const pre = pl.byId.get(id)
      if (!pre || pre === target) continue
      const spur = replayLayer.append('g').attr('class', 'tm-spur')
      spur.append('path').attr('d', pl.shortcut(target, pre))
      spur.append('circle').attr('cx', pre.x).attr('cy', pre.y).attr('r', 4)
      if (live) spur.style('opacity', 0).transition().delay(delay + HOP_MS).duration(150).style('opacity', 1)
    }
    const badge = replayLayer.append('g').attr('class', `tm-badge${i === count - 1 ? ' current' : ''}`).attr('transform', `translate(${target.x},${target.y})`)
    badge.append('circle').attr('r', 9)
    badge.append('text').attr('dy', '.35em').attr('text-anchor', 'middle').text(hop.label ?? i + 1)
    if (live) badge.style('opacity', 0).transition().delay(delay + HOP_MS).duration(150).style('opacity', 1)
  })
}

function play() {
  const all = props.replay?.length ?? 0
  drawHops(all, 0)
}

function reset() {
  replayLayer.selectAll('*').remove()
}

function fit() {
  const pl = placement.value
  const el = svgEl.value
  if (!pl || !el) return
  const { width, height } = el.getBoundingClientRect()
  if (!width || !height) return
  const { x, y, w, h } = pl.box
  const k = Math.min((width - 2 * FIT_PAD) / w, (height - 2 * FIT_PAD) / h, FIT_MAX)
  svg.call(behaviour.transform, zoomIdentity.translate(width / 2 - (x + w / 2) * k, height / 2 - (y + h / 2) * k).scale(k))
  moved = false
}

let drawn = 0

function syncReplay() {
  if (props.at === undefined) { drawn = 0; reset(); return }
  drawHops(props.at, props.live && props.at === drawn + 1 ? drawn : props.at)
  drawn = props.at
}

onMounted(() => {
  svg = select(svgEl.value!)
  canvas = svg.append('g')
  behaviour = zoom<SVGSVGElement, unknown>().scaleExtent([0.1, 6]).on('zoom', (e) => {
    canvas.attr('transform', e.transform.toString())
    if (e.sourceEvent) moved = true
  })
  svg.call(behaviour)
  draw()
  fit()
  syncReplay()
  resizer = new ResizeObserver(() => { if (!moved) fit() })
  resizer.observe(host.value!)
})

onBeforeUnmount(() => {
  resizer?.disconnect()
  svg?.interrupt()
  replayLayer?.selectAll('*').interrupt()
})

watch([placement, () => props.highlight], () => {
  if (!svg) return
  draw()
  syncReplay()
})
watch(placement, () => { if (svg) fit() })
watch(() => [props.replay, props.at], () => { if (svg) syncReplay() })

defineExpose({ fit, play, reset })
</script>

<template>
  <div ref="host" class="trailmap">
    <svg ref="svgEl" role="img" aria-label="Trail map of the project's signs and entries" />
    <button type="button" class="tm-fit" @click="fit">Fit</button>
    <p v-if="!placement" class="tm-empty muted">Loading</p>
  </div>
</template>

<style scoped>
.trailmap {
  position: relative;
  width: 100%;
  height: 100%;
  min-height: 24rem;
  background: var(--s1);
  border-radius: var(--radius);
  overflow: hidden;
}
svg { display: block; width: 100%; height: 100%; min-height: inherit; cursor: grab; touch-action: none; }
svg:active { cursor: grabbing; }
.tm-fit {
  position: absolute; top: 0.6rem; right: 0.6rem;
  font: inherit; font-size: 12px; color: var(--fg); background: var(--s2);
  border: 0; border-radius: 6px; padding: 0.25rem 0.7rem; cursor: pointer;
}
.tm-empty { position: absolute; inset: 0; display: grid; place-items: center; margin: 0; }
:deep(.tm-grid) { stroke: var(--line); stroke-opacity: 0.45; }
:deep(.tm-casing path) { stroke: var(--fg); stroke-opacity: 0.5; stroke-linecap: round; }
:deep(.tm-roads path) { stroke-linecap: round; }
:deep(.tm-trailhead) { fill: var(--fg); stroke: var(--s1); stroke-width: 3; }
:deep(.tm-road-sign) { fill: var(--s1); stroke-width: 3; }
:deep(.tm-road-entry) { fill: var(--s1); stroke-width: 2.5; }
:deep(.tm-rings circle) { fill: none; stroke: var(--mute); stroke-opacity: 0.6; stroke-width: 1.5; stroke-dasharray: 1 7; stroke-linecap: round; }
:deep(.tm-hulls path) { fill-opacity: 0.07; stroke-opacity: 0.2; stroke-width: 24; stroke-linejoin: round; }
:deep(.tm-peninsulas path) { fill-opacity: 0.1; stroke-opacity: 0.35; stroke-width: 10; stroke-linejoin: round; }
:deep(.tm-typed path) { stroke: var(--mute); stroke-opacity: 0.25; stroke-width: 1; }
:deep(.tm-group) { fill: var(--s2); stroke: var(--fg); stroke-width: 1.5; stroke-dasharray: 3 2; }
:deep(.tm-hover path) { stroke: var(--accent); stroke-width: 1.8; stroke-opacity: 0.9; }
:deep(.tm-links path) { stroke-width: 2.4; stroke-opacity: 0.8; }
:deep(.tm-node:focus-visible) { outline: none; }
:deep(.tm-node) { cursor: pointer; }
:deep(.tm-node circle:not(.tm-road-entry):not(.tm-trailhead):not(.tm-group)), :deep(.tm-node rect:not(.tm-road-sign)) { stroke: var(--s1); stroke-width: 1.5; }
:deep(.tm-node.dim) { opacity: 0.45; }
:deep(.tm-node.lit circle), :deep(.tm-node.lit rect) { stroke: var(--fg); stroke-width: 2.5; }
:deep(.tm-label) {
  font-size: 11px; fill: var(--mute); font-family: var(--sans);
  paint-order: stroke; stroke: var(--s1); stroke-width: 3px; stroke-linejoin: round;
}
:deep(.tm-label.sign) { font-size: 12px; font-weight: 600; fill: var(--fg); }
:deep(.tm-node.lit .tm-label) { fill: var(--fg); font-weight: 600; }
:deep(.tm-start) { fill: none; stroke: var(--fg); stroke-width: 2; }
:deep(.tm-fork), :deep(.tm-walk) { fill: none; stroke: var(--fg); stroke-width: 4; stroke-linecap: round; }
:deep(.tm-fork) { stroke-opacity: 0.14; }
:deep(.tm-walk) { stroke-opacity: 0.75; }
:deep(.tm-spur path) { fill: none; stroke: var(--fg); stroke-opacity: 0.6; stroke-dasharray: 3 3; stroke-width: 1.5; }
:deep(.tm-spur circle) { fill: var(--s1); stroke: var(--fg); stroke-width: 1.5; }
:deep(.tm-badge circle) { fill: var(--fg); }
:deep(.tm-badge.current circle) { stroke: var(--accent); stroke-width: 3; }
:deep(.tm-badge text) { font: 500 10px var(--mono); fill: var(--s1); }
</style>
