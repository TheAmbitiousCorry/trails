import { forceCollide, forceLink, forceManyBody, forceSimulation, forceX, forceY, type SimulationLinkDatum, type SimulationNodeDatum } from 'd3-force'
import { hierarchy, packSiblings, tree, type HierarchyNode } from 'd3-hierarchy'
import { polygonHull } from 'd3-polygon'
import { curveBasis, line, linkHorizontal, linkRadial } from 'd3-shape'
import type { MapTreeNode } from './mapmodel'

export type LayoutKind = 'ridge' | 'topo' | 'cluster'

export interface Placed {
  id: string
  node: HierarchyNode<MapTreeNode>
  x: number
  y: number
  /** Radians clockwise from twelve o'clock; Topo only. */
  angle: number
  radius: number
  /** Index of the top-level trail the node sits on, for colour; -1 for the trailhead. */
  trail: number
  label: string
  /** Ridge column or Topo ring: a sub-sign sits one beyond its siblings' entries and its own entries one beyond that. */
  step: number
}

export interface Hull { trail: number; points: [number, number][]; peninsula: boolean }

export interface Placement {
  kind: LayoutKind
  root: HierarchyNode<MapTreeNode>
  placed: Placed[]
  /** Every node by id; an entry folded into a group resolves to the group's Placed. */
  byId: Map<string, Placed>
  /** Topo ring radii from the trailhead outward; empty for the other layouts. */
  rings: number[]
  /** Half-width of the Topo map grid; 0 for the other layouts. */
  grid: number
  /** Islands, then their peninsulas; Cluster only. */
  hulls: Hull[]
  /** The tree trail from a parent to a child; a sub-sign's trail bends to its siblings' column or ring through a phantom point, then runs straight. */
  link: (source: Placed, target: Placed) => string
  shortcut: (from: Placed, to: Placed) => string
  /** Extent of the labels as well as the nodes, for fit-to-view. */
  box: { x: number; y: number; w: number; h: number }
}

const ROW = 20
const LABEL_PX = 6.4
const LABEL_MAX_PX = 380
const COLUMN_GAP = 70
const FIRST_COLUMN = 150
const MIN_COLUMN = 120
const SUB_SIGN_ROWS = 4
const SIGN_PX = 7.2
const ENTRY_PX = 6.2
const FIRST_RING = 150
const RING_GAP = 110

const TOWN_PX = 6.3
const SIGN_TOWN_PX = 7.4
const ISLAND_MARGIN = 48
const ISLAND_PAD = 30
const PENINSULA_PAD = 18
const SETTLE_TICKS = 600
const LABEL_GAP = 3
const FINISH_PASSES = 40

export function short(s: string, n: number): string {
  return s.length > n ? `${s.slice(0, n - 1)}…` : s
}

/** A sign reads by the words before its colon; its full title is the tooltip. */
function signName(title: string): string {
  return title.split(':')[0]
}

function labelFor(d: HierarchyNode<MapTreeNode>, max: number): string {
  if (!d.parent && d.data.id === 'root') return 'Trailhead'
  return d.data.type === 'sign' ? signName(d.data.title) : short(d.data.title, max)
}

function trailIndex(d: HierarchyNode<MapTreeNode>): number {
  let cur = d
  while (cur.parent && cur.parent.parent) cur = cur.parent
  return cur.parent ? (cur.parent.children ?? []).indexOf(cur) : -1
}

/** A sub-sign is a fork below a domain; its trail and its subtree get the extra step. */
const isSubSign = (d: HierarchyNode<MapTreeNode>) => !!d.children && d.depth >= 2

/** A domain's entries sit one step after it, a sub-sign one step beyond those entries, and its own entries one beyond that. */
const stepOf = (d: HierarchyNode<MapTreeNode>) => (d.parent ? d.depth + d.ancestors().filter(isSubSign).length : 0)

/** Width the label needs: a fork's name sits centred above it, a leaf's title runs to its right. */
const labelPx = (d: HierarchyNode<MapTreeNode>, label: string) => (d.children ? label.length * SIGN_PX : Math.min(LABEL_MAX_PX, label.length * (d.data.type === 'sign' ? SIGN_PX : LABEL_PX)))

function ridge(root: HierarchyNode<MapTreeNode>): Placement {
  tree<MapTreeNode>()
    .nodeSize([ROW, 1])
    .separation((a, b) => (a.depth > 1 && (a.children || b.children) ? SUB_SIGN_ROWS : a.parent === b.parent ? 1 : 1.4))(root)
  const all = root.descendants()
  const top = Math.min(...all.map((d) => d.x!))
  const placed = all.map<Placed>((d) => ({ id: d.data.id, node: d, x: 0, y: d.x! - top + ROW, angle: 0, radius: 0, trail: trailIndex(d), label: labelFor(d, 58), step: stepOf(d) }))
  const widthOf = new Map(placed.map((p) => [p, labelPx(p.node, p.label)]))
  const last = Math.max(...placed.map((p) => p.step))
  // A column starts past the longest label in the column before it, so a fork's children never sit under their uncles' titles.
  const colX = [0]
  for (let c = 1; c <= last; c++) {
    const widest = Math.max(0, ...placed.filter((p) => p.step === c - 1).map((p) => widthOf.get(p)!))
    colX[c] = colX[c - 1] + (c === 1 ? FIRST_COLUMN : Math.max(MIN_COLUMN, widest + COLUMN_GAP))
  }
  for (const p of placed) p.x = colX[p.step]
  const byId = new Map(placed.map((p) => [p.id, p]))
  const right = Math.max(...placed.map((p) => p.x + (p.node.children ? widthOf.get(p)! / 2 : 11 + widthOf.get(p)!)))
  const bottom = Math.max(...placed.map((p) => p.y))
  const horizontal = linkHorizontal<unknown, { x: number; y: number }>().x((p) => p.x).y((p) => p.y)
  const curve = (s: { x: number; y: number }, t: { x: number; y: number }) => horizontal({ source: s, target: t } as never) ?? ''
  return {
    kind: 'ridge',
    root,
    placed,
    byId,
    rings: [],
    grid: 0,
    hulls: [],
    link: (s, t) => (isSubSign(t.node) ? `${curve(s, { x: colX[s.step + 1], y: t.y })}L${t.x},${t.y}` : curve(s, t)),
    shortcut: (a, b) => `M${a.x},${a.y}C${a.x + 80},${a.y} ${b.x - 80},${b.y} ${b.x},${b.y}`,
    box: { x: -30, y: 0, w: right + 60, h: bottom + ROW },
  }
}

function topo(root: HierarchyNode<MapTreeNode>): Placement {
  tree<MapTreeNode>().size([2 * Math.PI, 1]).separation((a, b) => (a.parent === b.parent ? 1 : 1.5) / a.depth)(root)
  const all = root.descendants()
  const labels = new Map(all.map((d) => [d, labelFor(d, 36)]))
  const titleWidth = (d: HierarchyNode<MapTreeNode>) => (d.parent ? labels.get(d)!.length * (d.data.type === 'sign' ? SIGN_PX : ENTRY_PX) : 0)
  const outermost = Math.max(...all.map(stepOf))
  // Each ring starts past the longest title on the ring before it.
  const ring = [0]
  for (let r = 1; r <= outermost; r++) {
    const widest = Math.max(0, ...all.filter((d) => stepOf(d) === r - 1).map(titleWidth))
    ring[r] = ring[r - 1] + widest + (r === 1 ? FIRST_RING : RING_GAP)
  }
  const R = ring[outermost] + Math.max(0, ...all.filter((d) => stepOf(d) === outermost).map(titleWidth)) + 30
  const placed = all.map<Placed>((d) => {
    const step = stepOf(d)
    const radius = ring[step]
    const angle = d.x!
    return { id: d.data.id, node: d, angle, radius, step, trail: trailIndex(d), x: Math.sin(angle) * radius, y: -Math.cos(angle) * radius, label: labels.get(d)! }
  })
  const byId = new Map(placed.map((p) => [p.id, p]))
  const radial = linkRadial<unknown, { angle: number; radius: number }>().angle((p) => p.angle).radius((p) => p.radius)
  const curve = (s: { angle: number; radius: number }, t: { angle: number; radius: number }) => radial({ source: s, target: t } as never) ?? ''
  const road = line().curve(curveBasis)
  return {
    kind: 'topo',
    root,
    placed,
    byId,
    rings: ring,
    grid: R,
    hulls: [],
    link: (s, t) => (isSubSign(t.node) ? `${curve(s, { angle: t.angle, radius: ring[s.step + 1] })}L${t.x},${t.y}` : curve(s, t)),
    // A typed link bends through the open country between rings rather than through the centre.
    shortcut: (a, b) => {
      const mid = (a.angle + b.angle) / 2 + (Math.abs(a.angle - b.angle) > Math.PI ? Math.PI : 0)
      const r = Math.min(a.radius, b.radius) * 0.72
      return road([[a.x, a.y], [Math.cos(mid - Math.PI / 2) * r, Math.sin(mid - Math.PI / 2) * r], [b.x, b.y]]) ?? ''
    },
    box: { x: -R, y: -R, w: 2 * R, h: 2 * R },
  }
}

interface Circle { x: number; y: number; r: number }
interface Peninsula extends Circle { members: HierarchyNode<MapTreeNode>[] }
interface Island extends Circle { id: string; rMain: number; subs: Peninsula[] }
interface Town extends SimulationNodeDatum { d: HierarchyNode<MapTreeNode>; half: number; island: Island; pen?: Peninsula }

/**
 * Each domain is an island, a circle sized by the area its names need; the circles are packed around the trailhead's own circle with a margin,
 * towns settle inside their circle spaced by label width, and a sub-sign's subtree is a padded circle hung off the island's far side that the
 * island's towns keep out of. The start is seeded and the settle is synchronous, so the drawing is the same on every load.
 */
function cluster(root: HierarchyNode<MapTreeNode>): Placement {
  const nodes = root.descendants()
  const placed = nodes.map<Placed>((d) => ({ id: d.data.id, node: d, x: 0, y: 0, angle: 0, radius: 0, trail: trailIndex(d), label: labelFor(d, 34), step: 0 }))
  const of = new Map(nodes.map((d, i) => [d, placed[i]]))
  const topOf = (d: HierarchyNode<MapTreeNode>) => d.ancestors().find((a) => a.depth === 1)?.data.id ?? ''
  const subOf = (d: HierarchyNode<MapTreeNode>) => d.ancestors().find(isSubSign)
  const lw = (d: HierarchyNode<MapTreeNode>) => (d.parent ? of.get(d)!.label.length * (d.children ? SIGN_TOWN_PX : TOWN_PX) : 60)
  const areaOf = (ms: HierarchyNode<MapTreeNode>[]) => ms.reduce((a, m) => a + (lw(m) + 30) * 26, 0)

  const domains = root.children ?? []
  const islands = domains.map<Island>((s) => {
    const all = nodes.filter((n) => n !== root && topOf(n) === s.data.id)
    const subs = all.filter(isSubSign).map<Peninsula>((ss) => {
      const members = ss.descendants()
      return { members, x: 0, y: 0, r: Math.sqrt(areaOf(members) / Math.PI) * 1.45 + 50 }
    })
    const rMain = Math.sqrt(areaOf(all.filter((n) => !subOf(n))) / Math.PI) * 1.45 + 70
    const widest = Math.max(0, ...subs.map((x) => x.r))
    return { id: s.data.id, rMain, subs, x: 0, y: 0, r: rMain + (widest ? widest * 1.6 : 0) }
  })
  const hub: Circle = { x: 0, y: 0, r: 70 }
  const packed: Circle[] = [hub, ...islands]
  for (const c of packed) c.r += ISLAND_MARGIN
  packSiblings(packed)
  for (const c of packed) c.r -= ISLAND_MARGIN
  const ex = [Math.min(...packed.map((c) => c.x - c.r)), Math.max(...packed.map((c) => c.x + c.r))]
  const ey = [Math.min(...packed.map((c) => c.y - c.r)), Math.max(...packed.map((c) => c.y + c.r))]
  for (const c of packed) { c.x -= (ex[0] + ex[1]) / 2; c.y -= (ey[0] + ey[1]) / 2 }
  for (const c of islands) {
    const dx = c.x - hub.x
    const dy = c.y - hub.y
    c.subs.forEach((sb, i) => {
      const a = Math.atan2(dy, dx) + (i - (c.subs.length - 1) / 2) * 0.7
      sb.x = c.x + Math.cos(a) * (c.rMain + sb.r * 0.55)
      sb.y = c.y + Math.sin(a) * (c.rMain + sb.r * 0.55)
    })
  }
  const island = new Map(islands.map((c) => [c.id, c]))
  const penOf = new Map<HierarchyNode<MapTreeNode>, Peninsula>()
  for (const c of islands) for (const sb of c.subs) for (const m of sb.members) penOf.set(m, sb)

  let seed = 7
  const rnd = () => { seed = (seed * 9301 + 49297) % 233280; return seed / 233280 }
  const towns = nodes.filter((d) => d !== root).map<Town>((d) => {
    const pen = penOf.get(d)
    const isl = island.get(topOf(d))!
    const c = pen ?? isl
    const rr = pen ? pen.r : isl.rMain
    const a = rnd() * 2 * Math.PI
    const r = d.children && d.depth === 1 ? 0 : rnd() * rr * 0.8
    return { d, island: isl, pen, half: d.children ? 0 : lw(d) / 2, x: c.x + Math.cos(a) * r, y: c.y + Math.sin(a) * r }
  })
  const townOf = new Map(towns.map((t) => [t.d, t]))
  type Link = SimulationLinkDatum<Town>
  const links = root.links().filter((l) => l.source !== root).map<Link>((l) => ({ source: townOf.get(l.source)!, target: townOf.get(l.target)! }))
  const centre = (t: Town) => t.pen ?? t.island
  const byIsland = new Map<Island, Town[]>()
  for (const t of towns) byIsland.set(t.island, [...(byIsland.get(t.island) ?? []), t])
  // A fork's name sits above its square, a leaf's to the right of its dot; the dot is drawn left of the town's centre, so the box is centred a little right of it.
  const box = (t: Town) => (t.d.children
    ? { cx: t.x!, cy: t.y! - 10, hw: Math.max(10, lw(t.d) / 2) + LABEL_GAP, hh: 18 }
    : { cx: t.x! + 9, cy: t.y!, hw: lw(t.d) / 2 + LABEL_GAP, hh: 7 + LABEL_GAP })
  // The collision circles keep names apart on average; this pass separates the label boxes themselves so no two names overlap.
  const separate = () => {
    for (const group of byIsland.values()) {
      for (let i = 0; i < group.length; i++) {
        for (let j = i + 1; j < group.length; j++) {
          const a = box(group[i])
          const b = box(group[j])
          const ox = a.hw + b.hw - Math.abs(a.cx - b.cx)
          const oy = a.hh + b.hh - Math.abs(a.cy - b.cy)
          if (ox <= 0 || oy <= 0) continue
          if (ox < oy) { const d = ((a.cx < b.cx ? -ox : ox) / 2); group[i].x! += d; group[j].x! -= d }
          else { const d = ((a.cy < b.cy ? -oy : oy) / 2); group[i].y! += d; group[j].y! -= d }
        }
      }
    }
  }
  // A town's name counts, so it is held inside its own circle by its label half-width: a peninsula town inside the peninsula, an island town inside the island and out of every peninsula.
  const bound = () => {
    for (const t of towns) {
      let x = t.x!
      let y = t.y!
      if (t.pen) {
        const lim = Math.max(10, t.pen.r - 30 - t.half)
        const dx = x - t.pen.x
        const dy = y - t.pen.y
        const n = Math.hypot(dx, dy)
        if (n > lim) { x = t.pen.x + (dx / n) * lim; y = t.pen.y + (dy / n) * lim }
      } else {
        const c = t.island
        const lim = Math.max(10, c.rMain - 44 - t.half)
        const dx = x - c.x
        const dy = y - c.y
        const n = Math.hypot(dx, dy)
        if (n > lim) { x = c.x + (dx / n) * lim; y = c.y + (dy / n) * lim }
        for (const s2 of c.subs) {
          const sx = x - s2.x
          const sy = y - s2.y
          const m = Math.hypot(sx, sy) || 1
          const keep = s2.r + 30 + t.half
          if (m < keep) { x = s2.x + (sx / m) * keep; y = s2.y + (sy / m) * keep }
        }
      }
      t.x = x
      t.y = y
    }
  }
  const sim = forceSimulation(towns)
    .force('link', forceLink<Town, Link>(links).distance((l) => ((l.target as Town).d.children ? 110 : 60)).strength(0.5))
    .force('charge', forceManyBody<Town>().strength((t) => (t.d.children ? -400 : -40)))
    .force('collide', forceCollide<Town>((t) => (t.d.children ? 40 : 10 + lw(t.d) / 2)).strength(1).iterations(4))
    .force('ax', forceX<Town>((t) => centre(t).x).strength((t) => (t.d.children ? 0.9 : 0.3)))
    .force('ay', forceY<Town>((t) => centre(t).y).strength((t) => (t.d.children ? 0.9 : 0.3)))
    .force('labels', separate)
    .force('bound', bound)
    .stop()
  for (let i = 0; i < SETTLE_TICKS; i++) sim.tick()
  // The last integration step can leave a box touching; a few passes of the two constraints close it.
  for (let i = 0; i < FINISH_PASSES; i++) { separate(); bound() }

  // The dot is drawn left of the label's centre so the label box is what was spaced.
  const pos = new Map<HierarchyNode<MapTreeNode>, { x: number; y: number }>([[root, { x: hub.x, y: hub.y }]])
  for (const t of towns) pos.set(t.d, { x: t.d.children ? t.x! : t.x! - lw(t.d) / 2 + 8, y: t.y! })
  for (const d of nodes) { const p = of.get(d)!; const q = pos.get(d)!; p.x = q.x; p.y = q.y }

  const reach = (n: HierarchyNode<MapTreeNode>, pad: number): [number, number][] => {
    const { x, y } = pos.get(n)!
    const right = n.children ? pad : lw(n) + pad
    return [[x - pad, y - pad], [x - pad, y + pad], [x + right, y - pad], [x + right, y + pad]]
  }
  const hulls: Hull[] = []
  for (const s of domains) {
    const pts = nodes.filter((n) => n !== root && topOf(n) === s.data.id).flatMap((n) => reach(n, ISLAND_PAD))
    for (const sb of island.get(s.data.id)!.subs) {
      for (let a = 0; a < 2 * Math.PI; a += 0.4) pts.push([sb.x + Math.cos(a) * (sb.r + ISLAND_PAD), sb.y + Math.sin(a) * (sb.r + ISLAND_PAD)])
    }
    const h = polygonHull(pts)
    if (h) hulls.push({ trail: trailIndex(s), points: h, peninsula: false })
  }
  for (const s of nodes.filter(isSubSign)) {
    const h = polygonHull(s.descendants().flatMap((n) => reach(n, PENINSULA_PAD)))
    if (h) hulls.push({ trail: trailIndex(s), points: h, peninsula: true })
  }

  const islandPts = hulls.filter((h) => !h.peninsula).flatMap((h) => h.points)
  const span = 12 + 4
  const x0 = Math.min(-20, ...islandPts.map((q) => q[0])) - span
  const y0 = Math.min(-20, ...islandPts.map((q) => q[1])) - span
  const x1 = Math.max(20, ...islandPts.map((q) => q[0])) + span
  const y1 = Math.max(20, ...islandPts.map((q) => q[1])) + span
  const straight = (a: Placed, b: Placed) => `M${a.x},${a.y}L${b.x},${b.y}`
  const bowed = (a: Placed, b: Placed) => {
    const mx = (a.x + b.x) / 2
    const my = (a.y + b.y) / 2
    return `M${a.x},${a.y}Q${mx - (b.y - a.y) * 0.15},${my + (b.x - a.x) * 0.15} ${b.x},${b.y}`
  }
  return { kind: 'cluster', root, placed, byId: new Map(placed.map((p) => [p.id, p])), rings: [], grid: 0, hulls, link: straight, shortcut: bowed, box: { x: x0, y: y0, w: x1 - x0, h: y1 - y0 } }
}

/** Lays the subtree out in one of the three shapes; the tree is cloned, so the shared model is never mutated. Group nodes already in `data` resolve their members in `byId`. */
export function place(kind: LayoutKind, data: MapTreeNode): Placement {
  const root = hierarchy<MapTreeNode>(data, (n) => n.children)
  const out = kind === 'ridge' ? ridge(root) : kind === 'topo' ? topo(root) : cluster(root)
  for (const p of out.placed) for (const m of p.node.data.members ?? []) out.byId.set(m, p)
  return out
}

/** Finds the subtree rooted at `id`, or the whole tree when the id is empty or unknown. */
export function subtree(data: MapTreeNode, id: string): MapTreeNode {
  if (!id) return data
  const stack = [data]
  for (let n = stack.pop(); n; n = stack.pop()) {
    if (n.id === id) return n
    stack.push(...n.children)
  }
  return data
}
