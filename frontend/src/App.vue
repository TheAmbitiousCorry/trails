<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { projectId } from './api'
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
const { title } = useGraph()

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
      <RouterLink :to="{ name: 'projects' }" class="brand">{{ title || 'Trails' }}</RouterLink>
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
