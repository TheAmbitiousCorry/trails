<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useApi, type MapNode, type MapResponse } from '../api'
import Load from '../components/Load.vue'
import MapNodeRow from '../components/MapNode.vue'

const route = useRoute()
const router = useRouter()
const tag = computed(() => String(route.query.tag ?? ''))
const within = computed(() => String(route.query.within ?? ''))
const heat = ref(false)

const { data, error, loading } = useApi<MapResponse>(() => {
  const q = new URLSearchParams()
  if (tag.value) q.set('tag', tag.value)
  if (within.value) q.set('within', within.value)
  return `/api/map${q.size ? `?${q}` : ''}`
})

function setQuery(key: 'tag' | 'within', value: string) {
  const query = { ...route.query }
  if (value) query[key] = value
  else delete query[key]
  router.replace({ query })
}

function maxReads(n: MapNode): number {
  return Math.max(n.reads_30d, ...n.children.map(maxReads))
}
const hottest = computed(() => (data.value ? Math.max(1, ...data.value.root.children.map(maxReads), 1) : 1))
</script>

<template>
  <h1>Map</h1>
  <Load :ready="!!data" :loading="loading" :error="error">
    <template v-if="data">
      <div class="controls">
        <label>Tag
          <select :value="tag" @change="setQuery('tag', ($event.target as HTMLSelectElement).value)">
            <option value="">All entries</option>
            <option v-for="t in data.tags" :key="t" :value="t">{{ t }}</option>
          </select>
        </label>
        <label>Within
          <select :value="within" @change="setQuery('within', ($event.target as HTMLSelectElement).value)">
            <option value="">Whole project</option>
            <option v-for="s in data.signs" :key="s.id" :value="s.id">{{ '  '.repeat(s.depth) }}{{ s.title }}</option>
          </select>
        </label>
        <label class="check"><input v-model="heat" type="checkbox"> Heat (reads, last 30 days)</label>
      </div>
      <p v-if="tag" class="muted">
        Virtual sign for tag <strong>{{ tag }}</strong><template v-if="within"> within {{ data.signs.find((s) => s.id === within)?.title }}</template>:
        {{ data.root.entries }} {{ data.root.entries === 1 ? 'entry' : 'entries' }} under their home signs.
      </p>
      <p class="legend" aria-label="Tier colours">
        <span class="tier tier-verified"><span class="dot"></span>verified</span>
        <span class="tier tier-provisional"><span class="dot"></span>provisional</span>
        <span class="tier tier-stale"><span class="dot"></span>stale</span>
      </p>
      <ul class="tree root-tree">
        <MapNodeRow :key="`${tag}|${within}`" :node="data.root" :depth="0" :heat="heat" :max-reads="hottest" :expand-all="!!tag" />
      </ul>
    </template>
  </Load>
</template>
