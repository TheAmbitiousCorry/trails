<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'

const links = [
  { to: '/', label: 'Map' },
  { to: '/graph', label: 'Graph' },
  { to: '/sessions', label: 'Sessions' },
  { to: '/gaps', label: 'Gaps' },
  { to: '/provisional', label: 'Provisional' },
  { to: '/tags', label: 'Tags' },
  { to: '/logs', label: 'Rejections' },
]

const open = ref(false)
const route = useRoute()
watch(() => route.fullPath, () => { open.value = false })
</script>

<template>
  <div class="shell">
    <button type="button" class="menu-toggle" aria-controls="sidebar" :aria-expanded="open" @click="open = !open">Menu</button>
    <button v-if="open" type="button" class="scrim open" aria-label="Close menu" tabindex="-1" @click="open = false"></button>
    <aside id="sidebar" class="sidebar" :class="{ open }" @keydown.esc="open = false">
      <RouterLink to="/" class="brand">Trails</RouterLink>
      <nav class="side-nav" aria-label="Pages">
        <RouterLink v-for="l in links" :key="l.to" :to="l.to" :exact-active-class="l.to === '/' ? 'current' : ''" :active-class="l.to === '/' ? '' : 'current'">{{ l.label }}</RouterLink>
      </nav>
    </aside>
    <main class="wrap article">
      <RouterView :key="$route.fullPath" />
    </main>
  </div>
</template>
