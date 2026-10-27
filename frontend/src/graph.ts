import { computed, ref } from 'vue'
import { getJSON, type GraphEdge, type GraphNode, type GraphResponse } from './api'
import { buildTree, forkProngs, homeIdOf, pathIds, type Fork, type MapTreeNode } from './mapmodel'

export interface TreeSign { id: string; title: string; signs: TreeSign[]; entries: GraphNode[] }

// Module-level so every caller shares one fetch per app load.
const nodes = ref<GraphNode[]>([])
const edges = ref<GraphEdge[]>([])
const loading = ref(false)
const error = ref('')
let loaded = false

async function load() {
  loaded = true
  loading.value = true
  try {
    const out = await getJSON<GraphResponse>('/api/graph')
    nodes.value = out.nodes
    edges.value = out.edges
    error.value = ''
  } catch (e) {
    loaded = false
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

const signTree = computed<TreeSign[]>(() => {
  const byId = new Map<string, TreeSign>()
  for (const n of nodes.value) if (n.type === 'sign') byId.set(n.id, { id: n.id, title: n.title, signs: [], entries: [] })
  const roots: TreeSign[] = []
  for (const n of nodes.value) {
    if (n.type === 'sign') {
      const self = byId.get(n.id)!
      const parent = n.parent ? byId.get(n.parent) : undefined
      if (parent) parent.signs.push(self)
      else roots.push(self)
    } else if (n.sign_id) {
      byId.get(n.sign_id)?.entries.push(n)
    }
  }
  // A single root sign is the project itself; the tree starts at its children, as the Map does.
  return roots.length === 1 ? roots[0].signs : roots
})

/** The whole map from the root sign down, entries as leaves; null until the graph loads. */
export function tree(): MapTreeNode | null {
  return buildTree(nodes.value, edges.value)
}

/** Ids from the root to `id` inclusive. */
export function pathTo(id: string): string[] {
  return pathIds(nodes.value, id)
}

/** The sign `id` hangs under; a sign is its own home. */
export function homeOf(id: string): string {
  return homeIdOf(nodes.value, id)
}

/** The prongs an agent sees at a sign, rebuilt from the graph. */
export function forkOf(signId: string): Fork {
  return forkProngs(nodes.value, signId)
}

/** Refetches the graph so the sidebar tree shows signs created since it loaded. */
export function invalidate(): Promise<void> {
  return load()
}

export function useGraph() {
  if (!loaded) void load()
  return { nodes, edges, loading, error, tree: signTree }
}
