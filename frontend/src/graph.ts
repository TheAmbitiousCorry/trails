import { computed, ref } from 'vue'
import { getJSON, type Tier } from './api'

export interface GraphNode {
  type: 'sign' | 'entry'
  id: string
  title: string
  parent?: string
  kind?: string
  tier?: Tier
  reads_30d?: number
  sign_id?: string
  tags?: string[]
}

export interface GraphEdge { from: string; to: string; relation: string }

export interface TreeSign { id: string; title: string; signs: TreeSign[]; entries: GraphNode[] }

interface GraphResponse { nodes: GraphNode[]; edges: GraphEdge[] }

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

const tree = computed<TreeSign[]>(() => {
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

export function useGraph() {
  if (!loaded) void load()
  return { nodes, edges, loading, error, tree }
}
