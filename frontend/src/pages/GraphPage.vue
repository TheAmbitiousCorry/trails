<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useApi, type GraphResponse, projectApi } from '../api'
import { homeOf, useGraph } from '../graph'
import { useLayout } from '../layoutChoice'
import LayoutSwitch from '../components/LayoutSwitch.vue'
import Load from '../components/Load.vue'
import TrailMap from '../components/TrailMap.vue'

const route = useRoute()
const router = useRouter()
const global = useGraph()

const one = (v: unknown) => (typeof v === 'string' ? v : '')
const focus = computed(() => one(route.query.focus))
const tag = computed(() => one(route.query.tag))

const { layout, choose } = useLayout()

function setQuery(key: string, value: string) {
  const q = { ...route.query }
  if (value) q[key] = value
  else delete q[key]
  void router.replace({ query: q })
}

// A tag keeps the entries carrying it and the signs above them; the server does the filtering.
const filtered = useApi<GraphResponse>(() => (tag.value ? projectApi(`/graph?tag=${encodeURIComponent(tag.value)}`) : null))
const data = computed(() => (tag.value ? (filtered.data.value ?? undefined) : undefined))
const ready = computed(() => (tag.value ? !!filtered.data.value : global.nodes.value.length > 0))
const loading = computed(() => (tag.value ? filtered.loading.value : global.loading.value))
const error = computed(() => (tag.value ? filtered.error.value : global.error.value))

// The tag list comes from the unfiltered graph so a filtered view can still switch tags.
const tags = computed(() => [...new Set(global.nodes.value.flatMap((n) => n.tags ?? []))].sort())

// An entry has no subtree, so focusing one draws its sign's subtree with the entry lit.
const root = computed(() => (focus.value ? homeOf(focus.value) : ''))
const lit = computed(() => (focus.value && focus.value !== root.value ? [focus.value] : []))

const open = (id: string) => {
  const sign = global.nodes.value.find((n) => n.id === id)?.type === 'sign'
  void router.push(sign ? { name: 'sign', params: { id } } : { name: 'entry', params: { id } })
}
</script>

<template>
  <h1>Graph</h1>
  <div class="controls">
    <LayoutSwitch :layout="layout" @choose="choose" />
    <label>Tag
      <select :value="tag" @change="setQuery('tag', ($event.target as HTMLSelectElement).value)">
        <option value="">All entries</option>
        <option v-for="t in tags" :key="t" :value="t">{{ t }}</option>
      </select>
    </label>
    <RouterLink v-if="focus" :to="{ name: 'graph', query: tag ? { tag } : {} }" class="whole-map">Whole map</RouterLink>
  </div>
  <p class="legend" aria-label="Legend">
    <span class="lg"><svg width="14" height="14" aria-hidden="true"><rect x="1" y="1" width="12" height="12" rx="3" class="lg-sign" /></svg>sign, coloured by trail</span>
    <span class="lg"><svg width="14" height="14" aria-hidden="true"><circle cx="7" cy="7" r="6" class="lg-verified" /></svg>verified entry</span>
    <span class="lg"><svg width="14" height="14" aria-hidden="true"><circle cx="7" cy="7" r="6" class="lg-provisional" /></svg>provisional</span>
    <span class="lg"><svg width="14" height="14" aria-hidden="true"><circle cx="7" cy="7" r="6" class="lg-stale" /></svg>stale</span>
    <span class="lg muted">entry size is reads in 30 days<template v-if="layout !== 'cluster'">; a dashed ring is a group of entries, click it to open its sign</template>; hover a node to light its typed links</span>
  </p>
  <Load :ready="ready" :loading="loading" :error="error">
    <div class="graph-map">
      <TrailMap :layout="layout" :root="root" :highlight="lit" :data="data" @open="open" />
    </div>
  </Load>
</template>

<style scoped>
.whole-map { padding-bottom: 0.45rem; }
.lg { display: inline-flex; align-items: center; gap: 0.3rem; font-size: 0.8rem; color: var(--mute); }
.lg-sign { fill: var(--accent); }
.lg-verified { fill: var(--verified); }
.lg-provisional { fill: var(--provisional); }
.lg-stale { fill: var(--stale); }
/* Tall enough that a fitted map keeps labels readable; the page scrolls to it. */
.graph-map { height: max(70vh, 52rem); }
</style>
