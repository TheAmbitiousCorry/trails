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

export interface ReplayHop { target: string; prerequisites?: string[] }

const props = defineProps<{
  layout: LayoutKind
  /** Sign id whose subtree is drawn; the whole map when empty. */
  root?: string
  highlight?: string[]
  /** Hops to draw, each walked from the drawn root to its target. */
  replay?: ReplayHop[]
  /** Hops drawn instantly, 0 to replay.length; leave unset to draw nothing until play() runs. */
  at?: number
  /** Replaces the shared graph, for a server-filtered copy. */
  data?: { nodes: GraphNode[]; edges: GraphEdge[] }
}>()
const emit = defineEmits<{ open: [id: string] }>()

const HOP_MS = 650
const FIT_PAD = 24
const SHORTCUT_OPACITY = 0.2

const shared = useGraph()
const nodes = computed(() => props.data?.nodes ?? shared.nodes.value)
const edges = computed(() => props.data?.edges ?? shared.edges.value)
const placement = computed<Placement | null>(() => {
  const whole = buildTree(nodes.value, edges.value)
  return whole ? place(props.layout, subtree(whole, props.root ?? '')) : null
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
const tierColour = (t?: string) => (t === 'verified' ? 'var(--verified)' : t === 'stale' ? 'var(--stale)' : 'var(--provisional)')
const nodeSize = (p: Placed) => 4 + Math.sqrt(p.node.data.reads)

function draw() {
  const pl = placement.value
  canvas.selectAll('*').remove()
  replayLayer = canvas.append('g').attr('class', 'tm-replay')
  if (!pl) return
  const lit = new Set(props.highlight ?? [])
  const dimmed = lit.size > 0

  const back = canvas.insert('g', '.tm-replay').attr('class', 'tm-back')
  back.append('g').selectAll('circle').data(pl.rings).join('circle').attr('class', 'tm-ring').attr('r', (r) => r)
  const typed = edges.value.filter((e) => e.relation !== 'home' && pl.byId.has(e.from) && pl.byId.has(e.to))
  back.append('g').attr('class', 'tm-shortcuts').selectAll('path').data(typed).join('path')
    .attr('fill', 'none').attr('stroke-opacity', SHORTCUT_OPACITY)
    .attr('d', (e) => pl.shortcut(pl.byId.get(e.from)!, pl.byId.get(e.to)!))
  back.append('g').attr('class', 'tm-links').selectAll('path').data(pl.root.links()).join('path')
    .attr('fill', 'none')
    .attr('stroke', (l) => trailColour(pl.byId.get(l.target.data.id)!))
    .attr('d', (l) => pl.link(pl.byId.get(l.source.data.id)!, pl.byId.get(l.target.data.id)!))

  const topo = pl.kind === 'topo'
  const g = back.append('g').selectAll<SVGGElement, Placed>('g').data(pl.placed).join('g')
    .attr('class', (p) => `tm-node${lit.has(p.id) ? ' lit' : dimmed ? ' dim' : ''}`)
    .attr('transform', (p) => (topo ? `rotate(${(p.angle * 180) / Math.PI - 90}) translate(${p.radius},0)` : `translate(${p.x},${p.y})`))
    .on('click', (_, p) => { if (p.id !== 'root') emit('open', p.id) })
  g.append('title').text((p) => p.node.data.title)
  g.filter((p) => p.node.data.type === 'sign').append('rect')
    .attr('x', -6).attr('y', -6).attr('width', 12).attr('height', 12).attr('rx', 2).attr('fill', trailColour)
  g.filter((p) => p.node.data.type === 'entry').append('circle')
    .attr('r', nodeSize).attr('fill', (p) => tierColour(p.node.data.tier))
  g.append('text')
    .attr('class', (p) => `tm-label${p.node.data.type === 'sign' ? ' sign' : ''}`)
    .text((p) => p.label)
    .each(function (p) {
      const t = select(this)
      const internal = !!p.node.children
      if (topo) {
        const flip = p.angle >= Math.PI
        const outward = (p.angle < Math.PI) === !internal
        t.attr('dy', '.32em').attr('x', outward ? 9 : -9).attr('text-anchor', outward ? 'start' : 'end').attr('transform', flip ? 'rotate(180)' : null)
      } else {
        t.attr('dy', internal ? '-0.9em' : '.32em').attr('x', internal ? 0 : 11 + (p.node.data.type === 'entry' ? nodeSize(p) - 4 : 0)).attr('text-anchor', internal ? 'middle' : 'start')
      }
    })
}

function hopChain(pl: Placement, target: string): Placed[] {
  const p = pl.byId.get(target)
  return p ? p.node.ancestors().reverse().map((d) => pl.byId.get(d.data.id)!) : []
}

/** Draws hops [0, count) from the drawn root; animated hops start HOP_MS apart. */
function drawHops(count: number, animate: boolean) {
  replayLayer.selectAll('*').remove()
  const pl = placement.value
  const hops = props.replay ?? []
  if (!pl || count <= 0) return
  const live = animate && !reduceMotion()
  const root = pl.byId.get(pl.root.data.id)!
  replayLayer.append('circle').attr('class', 'tm-start').attr('cx', root.x).attr('cy', root.y).attr('r', 11)
  hops.slice(0, count).forEach((hop, i) => {
    const chain = hopChain(pl, hop.target)
    if (chain.length === 0) return
    const delay = live ? i * HOP_MS : 0
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
    const badge = replayLayer.append('g').attr('class', 'tm-badge').attr('transform', `translate(${target.x},${target.y})`)
    badge.append('circle').attr('r', 9)
    badge.append('text').attr('dy', '.35em').attr('text-anchor', 'middle').text(i + 1)
    if (live) badge.style('opacity', 0).transition().delay(delay + HOP_MS).duration(150).style('opacity', 1)
  })
}

function play() {
  drawHops(props.replay?.length ?? 0, true)
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

function syncReplay() {
  if (props.at !== undefined) drawHops(props.at, false)
  else reset()
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
:deep(.tm-ring) { fill: none; stroke: var(--line); stroke-dasharray: 2 6; }
:deep(.tm-shortcuts path) { stroke: var(--muted); stroke-dasharray: 3 4; }
:deep(.tm-links path) { stroke-width: 2.4; stroke-opacity: 0.8; }
:deep(.tm-node) { cursor: pointer; }
:deep(.tm-node circle), :deep(.tm-node rect) { stroke: var(--surface); stroke-width: 1.5; }
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
:deep(.tm-badge text) { font: 600 10px ui-monospace, monospace; fill: var(--surface); }
</style>
