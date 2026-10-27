import { forceCollide, forceLink, forceManyBody, forceSimulation, forceX, forceY, type SimulationLinkDatum, type SimulationNodeDatum } from 'd3-force'
import { hierarchy, tree, type HierarchyNode } from 'd3-hierarchy'
import { polygonHull } from 'd3-polygon'
import { linkHorizontal, linkRadial } from 'd3-shape'
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
}

export interface Hull { trail: number; points: [number, number][] }

export interface Placement {
  kind: LayoutKind
  root: HierarchyNode<MapTreeNode>
  placed: Placed[]
  byId: Map<string, Placed>
  /** Half-width of the Topo map grid; 0 for the other layouts. */
  grid: number
  /** One soft region per top-level trail; Cluster only. */
  hulls: Hull[]
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
const CLUSTER_STEPS = 300

export function short(s: string, n: number): string {
  return s.length > n ? `${s.slice(0, n - 1)}…` : s
}

/** A sign reads by the words before its colon; its full title is the tooltip. */
function signName(title: string): string {
  return title.split(':')[0]
}

function labelFor(d: HierarchyNode<MapTreeNode>, isRoot: boolean, max: number): string {
  if (isRoot && d.data.id === 'root') return 'Trailhead'
  return d.data.type === 'sign' ? signName(d.data.title) : short(d.data.title, max)
}

function trailIndex(d: HierarchyNode<MapTreeNode>): number {
  let cur = d
  while (cur.parent && cur.parent.parent) cur = cur.parent
  return cur.parent ? (cur.parent.children ?? []).indexOf(cur) : -1
}

function labelWidth(p: Placed, d: HierarchyNode<MapTreeNode>): number {
  return d.children ? 0 : Math.min(LABEL_MAX_PX, p.label.length * LABEL_PX)
}

function ridge(root: HierarchyNode<MapTreeNode>): Placement {
  tree<MapTreeNode>()
    .nodeSize([ROW, 1])
    .separation((a, b) => (a.depth > 1 && (a.children || b.children) ? SUB_SIGN_ROWS : a.parent === b.parent ? 1 : 1.4))(root)
  const all = root.descendants()
  const top = Math.min(...all.map((d) => d.x!))
  const maxDepth = Math.max(...all.map((d) => d.depth))
  const placed = all.map<Placed>((d) => ({ id: d.data.id, node: d, x: 0, y: d.x! - top + ROW, angle: 0, radius: 0, trail: trailIndex(d), label: labelFor(d, d === root, 58) }))
  // A column is as far right as the longest label in the column before it, so a fork's children never sit under their uncles' titles.
  const colX = [0]
  for (let depth = 1; depth <= maxDepth; depth++) {
    const widest = Math.max(0, ...placed.filter((p) => p.node.depth === depth - 1).map((p) => labelWidth(p, p.node)))
    colX[depth] = colX[depth - 1] + (depth === 1 ? FIRST_COLUMN : Math.max(MIN_COLUMN, widest + COLUMN_GAP))
  }
  for (const p of placed) p.x = colX[p.node.depth]
  const byId = new Map(placed.map((p) => [p.id, p]))
  const right = Math.max(...placed.map((p) => p.x + 11 + labelWidth(p, p.node)))
  const bottom = Math.max(...placed.map((p) => p.y))
  const horizontal = linkHorizontal<unknown, Placed>().x((p) => p.x).y((p) => p.y)
  return {
    kind: 'ridge',
    root,
    placed,
    byId,
    grid: 0,
    hulls: [],
    link: (s, t) => horizontal({ source: s, target: t } as never) ?? '',
    shortcut: (a, b) => `M${a.x},${a.y}C${a.x + 80},${a.y} ${b.x - 80},${b.y} ${b.x},${b.y}`,
    box: { x: -30, y: 0, w: right + 60, h: bottom + ROW },
  }
}

// A domain's entries sit on the ring just outside it, a sub-sign one ring beyond those entries and its own entries one beyond that;
// each ring starts past the longest title on the ring before it.
function topo(root: HierarchyNode<MapTreeNode>): Placement {
  tree<MapTreeNode>().size([2 * Math.PI, 1]).separation((a, b) => (a.parent === b.parent ? 1 : 1.5) / a.depth)(root)
  const all = root.descendants()
  const ringOf = (d: HierarchyNode<MapTreeNode>) => (d === root ? 0 : d.depth + d.ancestors().filter((a) => a.depth >= 2 && a.children).length)
  const labels = new Map(all.map((d) => [d, labelFor(d, d === root, 36)]))
  const titleWidth = (d: HierarchyNode<MapTreeNode>) => (d === root ? 0 : labels.get(d)!.length * (d.data.type === 'sign' ? SIGN_PX : ENTRY_PX))
  const outermost = Math.max(...all.map(ringOf))
  const ring = [0]
  for (let r = 1; r <= outermost; r++) {
    const widest = Math.max(0, ...all.filter((d) => ringOf(d) === r - 1).map(titleWidth))
    ring[r] = ring[r - 1] + widest + (r === 1 ? FIRST_RING : RING_GAP)
  }
  const R = ring[outermost] + Math.max(0, ...all.filter((d) => ringOf(d) === outermost).map(titleWidth)) + 30
  const placed = all.map<Placed>((d) => {
    const radius = ring[ringOf(d)]
    const angle = d.x!
    return { id: d.data.id, node: d, angle, radius, trail: trailIndex(d), x: Math.sin(angle) * radius, y: -Math.cos(angle) * radius, label: labels.get(d)! }
  })
  const byId = new Map(placed.map((p) => [p.id, p]))
  const radial = linkRadial<unknown, Placed>().angle((p) => p.angle).radius((p) => p.radius)
  return {
    kind: 'topo',
    root,
    placed,
    byId,
    grid: R,
    hulls: [],
    link: (s, t) => radial({ source: s, target: t } as never) ?? '',
    shortcut: (a, b) => `M${a.x},${a.y}Q0,0 ${b.x},${b.y}`,
    box: { x: -R, y: -R, w: 2 * R, h: 2 * R },
  }
}

interface SimNode extends SimulationNodeDatum { p: Placed; r: number }

/** Force layout with each top-level trail pulled to its own island; positions are settled here, so the drawing is static. */
function cluster(root: HierarchyNode<MapTreeNode>, typed: TypedLink[]): Placement {
  const all = root.descendants()
  const placed = all.map<Placed>((d) => ({ id: d.data.id, node: d, x: 0, y: 0, angle: 0, radius: 0, trail: trailIndex(d), label: labelFor(d, d === root, 30) }))
  const byId = new Map(placed.map((p) => [p.id, p]))
  const trails = Math.max(1, (root.children ?? []).length)
  const spread = 90 + 14 * Math.sqrt(all.length)
  const anchor = (n: SimNode) => {
    const a = (Math.max(n.p.trail, 0) / trails) * 2 * Math.PI
    return n.p.trail < 0 ? [0, 0] : [Math.cos(a) * spread, Math.sin(a) * spread]
  }
  const nodes = placed.map<SimNode>((p) => ({ p, r: p.node.data.type === 'sign' ? 14 : 5 + Math.sqrt(p.node.data.reads) }))
  const index = new Map(nodes.map((n) => [n.p.id, n]))
  type Link = SimulationLinkDatum<SimNode> & { home: boolean }
  const links: Link[] = [
    ...root.links().map<Link>((l) => ({ source: index.get(l.source.data.id)!, target: index.get(l.target.data.id)!, home: true })),
    ...typed.filter((e) => index.has(e.from) && index.has(e.to)).map<Link>((e) => ({ source: index.get(e.from)!, target: index.get(e.to)!, home: false })),
  ]
  const sim = forceSimulation(nodes)
    .force('link', forceLink<SimNode, Link>(links).distance((l) => (l.home ? 34 : 70)).strength((l) => (l.home ? 0.9 : 0.1)))
    .force('charge', forceManyBody().strength(-90))
    .force('collide', forceCollide<SimNode>((n) => n.r + 3))
    .force('x', forceX<SimNode>((n) => anchor(n)[0]).strength(0.14))
    .force('y', forceY<SimNode>((n) => anchor(n)[1]).strength(0.14))
    .stop()
  for (let i = 0; i < CLUSTER_STEPS; i++) sim.tick()
  for (const n of nodes) { n.p.x = n.x ?? 0; n.p.y = n.y ?? 0 }

  const hulls: Hull[] = []
  for (let t = 0; t < trails; t++) {
    const hull = polygonHull(placed.filter((p) => p.trail === t).map<[number, number]>((p) => [p.x, p.y]))
    if (hull) hulls.push({ trail: t, points: hull })
  }
  const pad = 40
  const labelled = placed.filter((p) => p.node.data.type === 'sign')
  const x0 = Math.min(...placed.map((p) => p.x)) - pad
  const y0 = Math.min(...placed.map((p) => p.y)) - pad
  const x1 = Math.max(...placed.map((p) => p.x + (labelled.includes(p) ? 12 + p.label.length * SIGN_PX : 0))) + pad
  const y1 = Math.max(...placed.map((p) => p.y)) + pad
  const line = (a: Placed, b: Placed) => `M${a.x},${a.y}L${b.x},${b.y}`
  return { kind: 'cluster', root, placed, byId, grid: 0, hulls, link: line, shortcut: line, box: { x: x0, y: y0, w: x1 - x0, h: y1 - y0 } }
}

export interface TypedLink { from: string; to: string }

/** Lays the subtree out in one of the three shapes; the tree is cloned, so the shared model is never mutated. `typed` only shapes Cluster. */
export function place(kind: LayoutKind, data: MapTreeNode, typed: TypedLink[] = []): Placement {
  const root = hierarchy<MapTreeNode>(data, (n) => n.children)
  return kind === 'ridge' ? ridge(root) : kind === 'topo' ? topo(root) : cluster(root, typed)
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
