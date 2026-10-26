<script setup lang="ts">
const links = [
  { to: '/', label: 'Map' },
  { to: '/sessions', label: 'Sessions' },
  { to: '/gaps', label: 'Gaps' },
  { to: '/provisional', label: 'Provisional' },
  { to: '/tags', label: 'Tags' },
  { to: '/logs', label: 'Rejections' },
]
</script>

<template>
  <header class="bar">
    <div class="wrap bar-inner">
      <RouterLink to="/" class="brand">Trails</RouterLink>
      <nav aria-label="Pages">
        <RouterLink v-for="l in links" :key="l.to" :to="l.to" :exact-active-class="l.to === '/' ? 'current' : ''" :active-class="l.to === '/' ? '' : 'current'">{{ l.label }}</RouterLink>
      </nav>
    </div>
  </header>
  <main class="wrap">
    <RouterView :key="$route.fullPath" />
  </main>
</template>
