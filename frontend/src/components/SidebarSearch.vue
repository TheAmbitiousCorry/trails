<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { getJSON, type SearchHit, type SearchResponse, projectApi } from '../api'

const query = defineModel<string>({ required: true })

const MAX_HITS = 8

const hits = ref<SearchHit[]>([])
const miss = ref(false)
const error = ref('')
let timer: ReturnType<typeof setTimeout> | undefined
let latest = 0
const shown = computed(() => hits.value.slice(0, MAX_HITS))

async function run(q: string) {
  const mine = ++latest
  try {
    const out = await getJSON<SearchResponse>(projectApi(`/search?q=${encodeURIComponent(q)}`))
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
    <!-- Hits and the slotted tree share this one scroll container so they can never overlap. -->
    <div class="side-scroll">
      <p v-if="error" class="notice error small" role="alert">{{ error }}</p>
      <template v-else-if="hits.length">
        <h2 id="side-hits-h" class="side-head">Matches</h2>
        <ul class="side-hits" aria-labelledby="side-hits-h">
          <li v-for="h in shown" :key="h.id">
            <RouterLink class="side-hit" :to="{ name: 'entry', params: { id: h.id } }">
              <span class="side-hit-title"><span class="disc small-disc" :class="`bg-${h.tier}`" :title="h.tier"></span>{{ h.title }}</span>
              <span v-if="h.sign_path.length" class="muted small">{{ h.sign_path.join(' / ') }}</span>
            </RouterLink>
          </li>
          <li v-if="hits.length > MAX_HITS" class="muted small side-note">{{ hits.length - MAX_HITS }} more in search</li>
        </ul>
      </template>
      <p v-else-if="miss" class="muted small side-note">No search hits.</p>
      <slot />
    </div>
  </div>
</template>
