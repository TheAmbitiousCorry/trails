<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { getJSON, type SearchHit, type SearchResponse } from '../api'

const query = defineModel<string>({ required: true })

const hits = ref<SearchHit[]>([])
const miss = ref(false)
const error = ref('')
let timer: ReturnType<typeof setTimeout> | undefined
let latest = 0

async function run(q: string) {
  const mine = ++latest
  try {
    const out = await getJSON<SearchResponse>(`/api/search?q=${encodeURIComponent(q)}`)
    if (mine !== latest) return
    hits.value = out.hits
    miss.value = out.hits.length === 0
    error.value = ''
  } catch (e) {
    if (mine !== latest) return
    hits.value = []
    miss.value = false
    error.value = e instanceof Error ? e.message : String(e)
  }
}

watch(query, (value) => {
  clearTimeout(timer)
  const q = value.trim()
  if (q.length < 3) {
    latest++
    hits.value = []
    miss.value = false
    error.value = ''
    return
  }
  timer = setTimeout(() => void run(q), 200)
})
onBeforeUnmount(() => clearTimeout(timer))

// Escape closes the mobile menu only when the box is already empty.
function onEscape(e: KeyboardEvent) {
  if (!query.value) return
  e.stopPropagation()
  query.value = ''
}
</script>

<template>
  <div class="side-search" role="search">
    <input v-model="query" type="search" class="side-search-input" placeholder="Search" aria-label="Search signs and entries" autocomplete="off" @keydown.esc="onEscape" />
    <p v-if="error" class="notice error small" role="alert">{{ error }}</p>
    <ul v-else-if="hits.length" class="side-hits" aria-label="Search results">
      <li v-for="h in hits" :key="h.id">
        <RouterLink class="side-hit" :to="{ name: 'entry', params: { id: h.id } }">
          <span class="side-hit-title"><span class="disc small-disc" :class="`bg-${h.tier}`" :title="h.tier"></span>{{ h.title }}</span>
          <span v-if="h.sign_path.length" class="muted small">{{ h.sign_path.join(' / ') }}</span>
        </RouterLink>
      </li>
    </ul>
    <p v-else-if="miss" class="muted small side-note">No search hits.</p>
  </div>
</template>
