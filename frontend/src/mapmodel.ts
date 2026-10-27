import type { GraphEdge, GraphNode } from './api'

export interface MapTreeNode {
  id: string
  title: string
  type: 'sign' | 'entry'
  kind?: string
  tier?: GraphNode['tier']
  reads: number
  children: MapTreeNode[]
}

export interface Prong {
  id: string
  title: string
  kind: 'sign' | 'entries'
  count: number
  entryIds: string[]
}

export interface Fork { prongs: Prong[]; more: number }

export const FORK_CAP = 8

/** Root to leaves from the home edges; signs sort before entries, then by title, so the drawing never reshuffles between loads. */
export function buildTree(nodes: GraphNode[], edges: GraphEdge[]): MapTreeNode | null {
  const made = new Map<string, MapTreeNode>()
  for (const n of nodes) {
    made.set(n.id, { id: n.id, title: n.title, type: n.type, kind: n.kind, tier: n.tier, reads: n.reads_30d ?? 0, children: [] })
  }
  const hasParent = new Set<string>()
  for (const e of edges) {
    if (e.relation !== 'home') continue
    const from = made.get(e.from)
    const to = made.get(e.to)
    if (!from || !to || hasParent.has(e.to)) continue
    from.children.push(to)
    hasParent.add(e.to)
  }
  for (const n of made.values()) {
    n.children.sort((a, b) => (a.type === b.type ? 0 : a.type === 'sign' ? -1 : 1) || a.title.localeCompare(b.title))
  }
  const roots = [...made.values()].filter((n) => n.type === 'sign' && !hasParent.has(n.id))
  return roots.find((n) => n.id === 'root') ?? roots[0] ?? null
}

/** Ids from the root to `id` inclusive; empty when the id is not in the tree. */
export function pathIds(nodes: GraphNode[], id: string): string[] {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const out: string[] = []
  for (let cur = byId.get(id); cur; cur = byId.get(cur.type === 'entry' ? (cur.sign_id ?? '') : (cur.parent ?? ''))) {
    if (out.includes(cur.id)) break
    out.unshift(cur.id)
  }
  return out
}

/** The sign an id hangs under: a sign is its own home, an entry's home is its sign. Empty when unknown. */
export function homeIdOf(nodes: GraphNode[], id: string): string {
  const n = nodes.find((x) => x.id === id)
  if (!n) return ''
  return n.type === 'sign' ? n.id : (n.sign_id ?? '')
}

/**
 * The forks an agent sees at a sign: child signs, then one prong per entry kind directly under it.
 * A lone summary is the sign's own description and costs nothing, as in signs.md.
 */
export function forkProngs(nodes: GraphNode[], signId: string): Fork {
  const all: Prong[] = []
  for (const n of nodes) {
    if (n.type === 'sign' && n.parent === signId) all.push({ id: n.id, title: n.title, kind: 'sign', count: 1, entryIds: [] })
  }
  const byKind = new Map<string, GraphNode[]>()
  for (const n of nodes) {
    if (n.type !== 'entry' || n.sign_id !== signId) continue
    const kind = n.kind ?? 'entry'
    byKind.set(kind, [...(byKind.get(kind) ?? []), n])
  }
  for (const [kind, ms] of byKind) {
    if (kind === 'summary' && ms.length === 1) continue
    if (ms.length === 1) all.push({ id: ms[0].id, title: ms[0].title, kind: 'entries', count: 1, entryIds: [ms[0].id] })
    else all.push({ id: `${signId}/${kind}`, title: `${ms.length} ${kind} entries`, kind: 'entries', count: ms.length, entryIds: ms.map((m) => m.id) })
  }
  return { prongs: all.slice(0, FORK_CAP), more: Math.max(0, all.length - FORK_CAP) }
}
