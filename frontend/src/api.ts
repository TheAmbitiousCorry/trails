import { ref, watchEffect, type Ref } from 'vue'

export type Tier = 'verified' | 'provisional' | 'stale'

export interface TierCounts { verified: number; provisional: number; stale: number }

export interface MapEntry { id: string; title: string; kind: string; tier: Tier; reads_30d: number }

export interface MapNode {
  id: string
  title: string
  tiers: TierCounts
  entries: number
  own_entries: number
  stale: number
  open_gaps: number
  reads_30d: number
  children: MapNode[]
  matches: MapEntry[]
}

export interface MapResponse {
  project: { id: string; title: string }
  tag: string
  within: string
  root: MapNode
  tags: string[]
  signs: { id: string; title: string; depth: number }[]
}

export interface Edge {
  type: 'sign' | 'entry' | 'group' | 'folded'
  id?: string
  title: string
  read_when?: string
  scent?: string[]
  tier?: Tier
  kind?: string
  word_count?: number
  child_count?: number
  stale_count?: number
  member_count?: number
}

export interface SignView { sign_id: string; title: string; summary: string; parent?: string; edges: Edge[]; lines?: number }

export interface Anchor {
  id: number
  type: string
  path_or_url?: string
  range_start?: number
  range_end?: number
  ok: boolean
  last_checked_at?: string
}

export interface Step { id: string; title: string }

export interface SignEntry {
  id: string
  title: string
  kind: string
  tier: Tier
  read_when: string
  tags: string[]
  body_markdown: string
  body_problem?: string
  chain_length: number
  anchors: Anchor[]
  reads_30d: number
  updated_at: string
}

export interface SignResponse {
  view: SignView
  path: Step[]
  stale: number
  open_gaps: number
  groups: { kind: string; entries: SignEntry[] }[]
}

export interface LinkRow { relation: string; id: string; title: string; tier: Tier }

export interface EntryResponse {
  entry: SignEntry
  sign_id: string
  path: Step[]
  created_at: string
  superseded_by: string
  promoted_by: string
  decided_by: string
  decided_at: string
  chain: string[]
  versions: { id: number; actor: string; title: string; read_when: string; created_at: string }[]
  links: LinkRow[]
  backlinks: LinkRow[]
}

export interface Snippet {
  anchor_id: number
  path: string
  start: number
  end: number
  language: string
  text: string
  truncated: boolean
  ok: boolean
}

export interface SessionRow {
  id: string
  actor: string
  task: string
  task_type: string
  started_at: string
  ended_at: string
  ended_by: string
  counts: { looks: number; searches: number; reads: number; writes: number; confirms: number; off_trail: number; misses: number }
  open_gaps: number
}

export interface RouteStep {
  n: number
  at: string
  tool: string
  target: string
  title: string
  kind: 'sign' | 'entry' | 'query'
  depth?: string
  result: string
  miss: boolean
  gap_id?: number
  turn_back: boolean
  touched: boolean
  prerequisites: Step[]
  shown: Step[]
}

export interface SessionResponse {
  session: SessionRow
  route: RouteStep[]
  off_trail: { at: string; path: string; entry_id: string; title: string }[]
  touched: { id: string; title: string; tier: Tier; how: 'wrote' | 'confirmed'; at: string }[]
}

export interface Gap {
  id: number
  sign_id: string
  sign_title: string
  session_id: string
  task: string
  query: string
  paths: string
  state: string
  resolution: string
  target_id: string
  reason: string
  opened_at: string
  resolved_at: string
}

export interface ProvisionalRow {
  id: string
  title: string
  kind: string
  read_when: string
  sign_id: string
  path: Step[]
  reads: number
  reads_30d: number
  confirmed_by: number
  created_at: string
}

export interface TagRow { id: string; description: string; defined: boolean; uses: number }

export interface Rejections {
  total: number
  groups: { rule: string; actor_kind: string; count: number }[]
  recent: { at: string; tool: string; target: string; rule: string; actor_kind: string; session_id: string }[]
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

export async function getJSON<T>(url: string): Promise<T> {
  const res = await fetch(url)
  if (!res.ok) throw new ApiError(res.status, (await res.text()).trim() || res.statusText)
  return res.json() as Promise<T>
}

export interface Loaded<T> { data: Ref<T | null>; error: Ref<string>; loading: Ref<boolean> }

/** Fetches whenever the url changes; a null url means wait. A late reply for a stale url is dropped. */
export function useApi<T>(url: () => string | null): Loaded<T> {
  const data = ref<T | null>(null) as Ref<T | null>
  const error = ref('')
  const loading = ref(true)
  let latest = 0
  watchEffect(async () => {
    const u = url()
    if (u === null) return
    const mine = ++latest
    loading.value = true
    try {
      const out = await getJSON<T>(u)
      if (mine !== latest) return
      data.value = out
      error.value = ''
    } catch (e) {
      if (mine !== latest) return
      data.value = null
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      if (mine === latest) loading.value = false
    }
  })
  return { data, error, loading }
}

export function when(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}
