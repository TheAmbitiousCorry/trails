import { hierarchy, tree, type HierarchyNode } from 'd3-hierarchy'
import { linkHorizontal, linkRadial } from 'd3-shape'
import type { MapTreeNode } from './mapmodel'

export type LayoutKind = 'ridge' | 'topo'

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

export interface Placement {
  kind: LayoutKind
  root: HierarchyNode<MapTreeNode>
  placed: Placed[]
  byId: Map<string, Placed>
  rings: number[]
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
const RING = [0, 0.42, 0.8, 1]
const SUB_SIGN_BAND = 3.2

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
    rings: [],
    link: (s, t) => horizontal({ source: s, target: t } as never) ?? '',
    shortcut: (a, b) => `M${a.x},${a.y}C${a.x + 80},${a.y} ${b.x - 80},${b.y} ${b.x},${b.y}`,
    box: { x: -30, y: 0, w: right + 60, h: bottom + ROW },
  }
}

function topo(root: HierarchyNode<MapTreeNode>): Placement {
  const leaves = root.leaves().length
  const R = Math.max(300, leaves * 3)
  tree<MapTreeNode>()
    .size([2 * Math.PI, R])
    .separation((a, b) => ((a.depth > 1 && (a.children || b.children) ? SUB_SIGN_BAND : a.parent === b.parent ? 1 : 1.8) / a.depth))(root)
  const placed = root.descendants().map<Placed>((d) => {
    const radius = R * RING[Math.min(d.depth, 3)]
    const angle = d.x!
    return {
      id: d.data.id, node: d, angle, radius, trail: trailIndex(d),
      x: Math.sin(angle) * radius, y: -Math.cos(angle) * radius,
      label: labelFor(d, d === root, d.children ? 18 : 30),
    }
  })
  const byId = new Map(placed.map((p) => [p.id, p]))
  const radial = linkRadial<unknown, Placed>().angle((p) => p.angle).radius((p) => p.radius)
  const reach = R + 30 * LABEL_PX
  return {
    kind: 'topo',
    root,
    placed,
    byId,
    rings: RING.slice(1).map((r) => R * r),
    link: (s, t) => radial({ source: s, target: t } as never) ?? '',
    shortcut: (a, b) => `M${a.x},${a.y}Q0,0 ${b.x},${b.y}`,
    box: { x: -reach, y: -reach, w: 2 * reach, h: 2 * reach },
  }
}

/** Lays the subtree out in one of the two trail-map shapes; the tree is cloned, so the shared model is never mutated. */
export function place(kind: LayoutKind, data: MapTreeNode): Placement {
  const root = hierarchy<MapTreeNode>(data, (n) => n.children)
  return kind === 'ridge' ? ridge(root) : topo(root)
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
