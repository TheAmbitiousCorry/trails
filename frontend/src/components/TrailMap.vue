<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { easeLinear } from 'd3-ease'
import { select, type Selection } from 'd3-selection'
import { zoom, zoomIdentity, type ZoomBehavior } from 'd3-zoom'
import 'd3-transition'
import type { GraphEdge, GraphNode } from '../api'
import { useGraph } from '../graph'
import { place, subtree, type LayoutKind, type Placed, type Placement } from '../mapLayout'
import { buildTree } from '../mapmodel'

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

const shared = useGraph()
const nodes = computed(() => props.data?.nodes ?? shared.nodes.value)
const edges = computed(() => props.data?.edges ?? shared.edges.value)
const typedEdges = computed(() => edges.value.filter((e) => e.relation !== 'home'))
const placement = computed<Placement | null>(() => {
  const whole = buildTree(nodes.value, edges.value)
  return whole ? place(props.layout, subtree(whole, props.root ?? ''), typedEdges.value) : null
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
const trailColour = (p: Placed) => (p.trail < 0 ? 'var(--text)' : `var(--trail-${(p.trail % 8) + 1})`)
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
  const lit = new Set(props.highlight ?? [])
  const dimmed = lit.size > 0
  const topo = pl.kind === 'topo'
  const cluster = pl.kind === 'cluster'
  const links = pl.root.links()
  const typed = typedEdges.value.filter((e) => pl.byId.has(e.from) && pl.byId.has(e.to))

  const back = canvas.insert('g', '.tm-replay').attr('class', 'tm-back')
  if (topo) {
    const grid = back.append('g').attr('class', 'tm-grid')
    for (let v = -pl.grid; v <= pl.grid; v += GRID_STEP) {
      grid.append('line').attr('x1', v).attr('x2', v).attr('y1', -pl.grid).attr('y2', pl.grid)
      grid.append('line').attr('y1', v).attr('y2', v).attr('x1', -pl.grid).attr('x2', pl.grid)
    }
  }
  if (cluster) {
    back.append('g').attr('class', 'tm-hulls').selectAll('path').data(pl.hulls).join('path')
      .attr('d', (h) => `M${h.points.map((q) => q.join(',')).join('L')}Z`).attr('fill', (h) => trailVar(h.trail)).attr('stroke', (h) => trailVar(h.trail))
    back.append('g').attr('class', 'tm-shortcuts').selectAll('path').data(typed).join('path')
      .attr('fill', 'none').attr('d', (e) => pl.shortcut(pl.byId.get(e.from)!, pl.byId.get(e.to)!))
  }
  if (topo) {
    back.append('g').attr('class', 'tm-casing').selectAll('path').data(links).join('path')
      .attr('fill', 'none').attr('stroke-width', (l) => roadWidth(l) + 3).attr('d', (l) => pl.link(pl.byId.get(l.source.data.id)!, pl.byId.get(l.target.data.id)!))
    back.append('g').attr('class', 'tm-roads').selectAll('path').data(links).join('path')
      .attr('fill', 'none').attr('stroke-width', roadWidth)
      .attr('stroke', (l) => (l.source.depth === 0 ? 'var(--bg)' : trailColour(pl.byId.get(l.target.data.id)!)))
      .attr('d', (l) => pl.link(pl.byId.get(l.source.data.id)!, pl.byId.get(l.target.data.id)!))
  } else {
    back.append('g').attr('class', 'tm-links').selectAll('path').data(links).join('path')
      .attr('fill', 'none')
      .attr('stroke', (l) => trailColour(pl.byId.get(l.target.data.id)!))
      .attr('d', (l) => pl.link(pl.byId.get(l.source.data.id)!, pl.byId.get(l.target.data.id)!))
  }
  const hover = back.append('g').attr('class', 'tm-hover')

  const g = back.append('g').selectAll<SVGGElement, Placed>('g').data(pl.placed).join('g')
    .attr('class', (p) => `tm-node${lit.has(p.id) ? ' lit' : dimmed ? ' dim' : ''}`)
    .attr('transform', (p) => `translate(${p.x},${p.y})`)
    .attr('tabindex', 0).attr('role', 'link').attr('aria-label', (p) => `${p.node.data.type} ${p.node.data.title}`)
    .on('click', (_, p) => { if (p.id !== 'root') emit('open', p.id) })
    .on('keydown', (ev: KeyboardEvent, p) => { if (ev.key === 'Enter' && p.id !== 'root') emit('open', p.id) })
    .on('mouseenter focus', (_, p) => {
      const mine = typed.filter((e) => e.from === p.id || e.to === p.id)
      hover.selectAll('path').data(mine).join('path').attr('fill', 'none').attr('d', (e) => pl.shortcut(pl.byId.get(e.from)!, pl.byId.get(e.to)!))
    })
    .on('mouseleave blur', () => hover.selectAll('*').remove())
  g.append('title').text((p) => p.node.data.title)
  if (topo) {
    g.filter((p) => p.node.data.id === pl.root.data.id).append('circle').attr('r', 13).attr('class', 'tm-trailhead')
    g.filter((p) => p.node.data.type === 'sign' && p.node.data.id !== pl.root.data.id).append('rect')
      .attr('x', -7).attr('y', -7).attr('width', 14).attr('height', 14).attr('rx', 3).attr('class', 'tm-road-sign').attr('stroke', trailColour)
    g.filter((p) => p.node.data.type === 'entry').append('circle')
      .attr('r', (p) => 4.5 + Math.sqrt(p.node.data.reads)).attr('class', 'tm-road-entry').attr('stroke', (p) => tierColour(p.node.data.tier))
  } else {
    g.filter((p) => p.node.data.type === 'sign').append('rect')
      .attr('x', -6).attr('y', -6).attr('width', 12).attr('height', 12).attr('rx', 2).attr('fill', trailColour)
    g.filter((p) => p.node.data.type === 'entry').append('circle')
      .attr('r', nodeSize).attr('fill', (p) => tierColour(p.node.data.tier))
  }
  g.append('text')
    .attr('class', (p) => `tm-label${p.node.data.type === 'sign' ? ' sign' : ''}${cluster && p.node.data.type === 'entry' ? ' hidden' : ''}`)
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
        t.attr('dy', '.35em').attr('x', p.node.data.type === 'sign' ? 12 : nodeSize(p) + 4)
      } else {
        t.attr('dy', internal ? '-0.9em' : '.32em').attr('x', internal ? 0 : 11 + (p.node.data.type === 'entry' ? nodeSize(p) - 4 : 0)).attr('text-anchor', internal ? 'middle' : 'start')
      }
    })
}

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
  const k = Math.min((width - 2 * FIT_PAD) / w, (height - 2 * FIT_PAD) / h, 1.5)
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
  --trail-1: #2f6b5a; --trail-2: #b87a08; --trail-3: #3b63c9; --trail-4: #c0452c;
  --trail-5: #7c4dbf; --trail-6: #1f8fa0; --trail-7: #b48a1a; --trail-8: #4f8f2a;
  position: relative;
  width: 100%;
  height: 100%;
  min-height: 24rem;
  background: var(--surface);
  border-radius: var(--radius);
  overflow: hidden;
}
@media (prefers-color-scheme: dark) {
  .trailmap {
    --trail-1: #79c4ac; --trail-2: #e0a53a; --trail-3: #7fa7ff; --trail-4: #ef7a62;
    --trail-5: #c99bff; --trail-6: #5fd0e0; --trail-7: #f0c674; --trail-8: #9ad66a;
  }
}
svg { display: block; width: 100%; height: 100%; min-height: inherit; cursor: grab; touch-action: none; }
svg:active { cursor: grabbing; }
.tm-fit {
  position: absolute; top: 0.6rem; right: 0.6rem;
  font: inherit; font-size: 0.85rem; color: var(--text); background: var(--bg);
  border: 1px solid var(--line); border-radius: 6px; padding: 0.25rem 0.7rem; cursor: pointer;
}
.tm-empty { position: absolute; inset: 0; display: grid; place-items: center; margin: 0; }
:deep(.tm-grid) { stroke: var(--line); stroke-opacity: 0.45; }
:deep(.tm-casing path) { stroke: var(--text); stroke-opacity: 0.5; stroke-linecap: round; }
:deep(.tm-roads path) { stroke-linecap: round; }
:deep(.tm-trailhead) { fill: var(--text); stroke: var(--surface); stroke-width: 3; }
:deep(.tm-road-sign) { fill: var(--surface); stroke-width: 3; }
:deep(.tm-road-entry) { fill: var(--surface); stroke-width: 2.5; }
:deep(.tm-hulls path) { fill-opacity: 0.06; stroke-opacity: 0.18; stroke-width: 18; stroke-linejoin: round; }
:deep(.tm-shortcuts path) { stroke: var(--muted); stroke-opacity: 0.25; stroke-width: 1; }
:deep(.tm-hover path) { stroke: var(--accent); stroke-width: 1.8; stroke-opacity: 0.9; }
:deep(.tm-links path) { stroke-width: 2.4; stroke-opacity: 0.8; }
:deep(.tm-label.hidden) { opacity: 0; }
:deep(.tm-node:hover .tm-label.hidden), :deep(.tm-node:focus .tm-label.hidden) { opacity: 1; }
:deep(.tm-node:focus-visible) { outline: none; }
:deep(.tm-node) { cursor: pointer; }
:deep(.tm-node circle:not(.tm-road-entry):not(.tm-trailhead)), :deep(.tm-node rect:not(.tm-road-sign)) { stroke: var(--surface); stroke-width: 1.5; }
:deep(.tm-node.dim) { opacity: 0.45; }
:deep(.tm-node.lit circle), :deep(.tm-node.lit rect) { stroke: var(--text); stroke-width: 2.5; }
:deep(.tm-label) {
  font-size: 11px; fill: var(--muted);
  paint-order: stroke; stroke: var(--surface); stroke-width: 3px; stroke-linejoin: round;
}
:deep(.tm-label.sign) { font-size: 12px; font-weight: 600; fill: var(--text); }
:deep(.tm-node.lit .tm-label) { fill: var(--text); font-weight: 600; }
:deep(.tm-start) { fill: none; stroke: var(--text); stroke-width: 2; }
:deep(.tm-fork), :deep(.tm-walk) { fill: none; stroke: var(--text); stroke-width: 4; stroke-linecap: round; }
:deep(.tm-fork) { stroke-opacity: 0.14; }
:deep(.tm-walk) { stroke-opacity: 0.75; }
:deep(.tm-spur path) { fill: none; stroke: var(--text); stroke-opacity: 0.6; stroke-dasharray: 3 3; stroke-width: 1.5; }
:deep(.tm-spur circle) { fill: var(--surface); stroke: var(--text); stroke-width: 1.5; }
:deep(.tm-badge circle) { fill: var(--text); }
:deep(.tm-badge.current circle) { stroke: var(--accent); stroke-width: 3; }
:deep(.tm-badge text) { font: 600 10px ui-monospace, monospace; fill: var(--surface); }
</style>
