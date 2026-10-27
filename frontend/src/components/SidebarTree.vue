<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useGraph, type TreeSign } from '../graph'

// Without a `signs` prop this is the tree root and reads the graph and route; branches receive both and render only <li>s.
const props = defineProps<{ signs?: TreeSign[]; current?: string; open?: Set<string>; query?: string; locked?: boolean }>()

const route = useRoute()
const { nodes, tree, loading, error } = useGraph()
const isRoot = computed(() => props.signs === undefined)

const current = computed(() => (route.name === 'sign' || route.name === 'entry' ? String(route.params.id ?? '') : ''))

const ancestors = computed(() => {
  const out = new Set<string>()
  const parent = new Map<string, string>()
  let sign = route.name === 'sign' ? current.value : ''
  for (const n of nodes.value) {
    if (n.type === 'sign' && n.parent) parent.set(n.id, n.parent)
    if (route.name === 'entry' && n.id === current.value) sign = n.sign_id ?? ''
  }
  for (let s = sign; s; s = parent.get(s) ?? '') out.add(s)
  return out
})

const ownOpen = ref(new Set<string>())
const open = computed(() => props.open ?? ownOpen.value)
// A route change opens the current page's ancestors; twists the reader set by hand stay as they were.
watch(ancestors, (a) => { for (const id of a) ownOpen.value.add(id) }, { immediate: true })

const needle = computed(() => (props.query ?? '').trim().toLowerCase())

// While a query is set the tree is the filtered copy, every branch with a match is open, and twists are inert.
const filtered = computed(() => {
  const open = new Set<string>()
  if (!needle.value) return { tree: tree.value, open }
  const has = (t: string) => t.toLowerCase().includes(needle.value)
  const prune = (signs: TreeSign[]): TreeSign[] => {
    const out: TreeSign[] = []
    for (const s of signs) {
      const kept: TreeSign = { id: s.id, title: s.title, signs: prune(s.signs), entries: s.entries.filter((e) => has(e.title)) }
      if (kept.signs.length || kept.entries.length) open.add(s.id)
      if (has(s.title) || kept.signs.length || kept.entries.length) out.push(kept)
    }
    return out
  }
  return { tree: prune(tree.value), open }
})
const filtering = computed(() => needle.value !== '')

function toggle(id: string) {
  if (props.locked) return
  if (open.value.has(id)) open.value.delete(id)
  else open.value.add(id)
}
const expandable = (s: TreeSign) => s.signs.length > 0 || s.entries.length > 0
</script>

<template>
  <template v-if="isRoot">
    <p v-if="error" class="notice error" role="alert">{{ error }}</p>
    <p v-else-if="loading && !tree.length" class="muted small side-note">Loading</p>
    <ul v-else-if="filtered.tree.length" class="side-tree" aria-label="Signs and entries">
      <SidebarTree :signs="filtered.tree" :current="current" :open="filtering ? filtered.open : ownOpen" :locked="filtering" />
    </ul>
    <p v-else-if="filtering" class="muted small side-note">No titles match.</p>
  </template>
  <template v-else>
    <li v-for="s in signs" :key="s.id">
      <div class="side-row">
        <button v-if="expandable(s)" type="button" class="twist" :disabled="locked" :aria-expanded="open.has(s.id)" :aria-label="`${open.has(s.id) ? 'Collapse' : 'Expand'} ${s.title}`" @click="toggle(s.id)">
          <span :class="{ turned: open.has(s.id) }" aria-hidden="true">&#9656;</span>
        </button>
        <span v-else class="twist-gap"></span>
        <RouterLink class="side-title" :to="{ name: 'sign', params: { id: s.id } }" :aria-current="current === s.id ? 'page' : undefined">{{ s.title }}</RouterLink>
      </div>
      <ul v-if="open.has(s.id) && expandable(s)" class="side-tree">
        <SidebarTree v-if="s.signs.length" :signs="s.signs" :current="current" :open="open" :locked="locked" />
        <li v-for="e in s.entries" :key="e.id" class="side-row side-entry">
          <RouterLink class="side-title" :to="{ name: 'entry', params: { id: e.id } }" :aria-current="current === e.id ? 'page' : undefined">{{ e.title }}</RouterLink>
        </li>
      </ul>
    </li>
  </template>
</template>
