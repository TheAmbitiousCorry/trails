<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { getJSON, projectId, type ProjectRow } from './api'
import { useGraph } from './graph'
import SidebarSearch from './components/SidebarSearch.vue'
import SidebarTree from './components/SidebarTree.vue'

const links = [
  { name: 'map', label: 'Map' },
  { name: 'graph', label: 'Graph' },
  { name: 'sessions', label: 'Sessions' },
  { name: 'gaps', label: 'Gaps' },
  { name: 'provisional', label: 'Provisional' },
  { name: 'tags', label: 'Tags' },
  { name: 'logs', label: 'Rejections' },
]
useGraph()

const projects = ref<ProjectRow[]>([])
const router = useRouter()
onMounted(async () => {
  try { projects.value = (await getJSON<{ projects: ProjectRow[] }>('/api/projects')).projects } catch { projects.value = [] }
})
function choose(e: Event) {
  router.push({ name: 'map', params: { project: (e.target as HTMLSelectElement).value } })
}

const open = ref(false)
const query = ref('')
const route = useRoute()
watch(() => route.fullPath, () => { open.value = false })
</script>

<template>
  <div class="shell">
    <button v-if="projectId" type="button" class="menu-toggle" aria-controls="sidebar" :aria-expanded="open" @click="open = !open">Menu</button>
    <button v-if="open" type="button" class="scrim open" aria-label="Close menu" tabindex="-1" @click="open = false"></button>
    <aside v-if="projectId" id="sidebar" :key="projectId" class="sidebar" :class="{ open }" @keydown.esc="open = false">
      <div class="brand">
        <select class="project-select" aria-label="Project" @change="choose">
          <option v-for="p in projects" :key="p.id" :value="p.id" :selected="p.id === projectId">{{ p.title }}</option>
        </select>
      </div>
      <SidebarSearch v-model="query">
        <nav class="side-tree-nav" aria-label="Wiki">
          <h2 v-if="query.trim()" class="side-head">In the tree</h2>
          <SidebarTree :query="query" />
        </nav>
      </SidebarSearch>
      <nav class="side-nav" aria-label="Pages">
        <RouterLink v-for="l in links" :key="l.name" :to="{ name: l.name }" :exact-active-class="l.name === 'map' ? 'current' : ''" :active-class="l.name === 'map' ? '' : 'current'">{{ l.label }}</RouterLink>
      </nav>
    </aside>
    <main class="wrap article">
      <RouterView :key="$route.fullPath" />
    </main>
  </div>
</template>
