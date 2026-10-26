<script setup lang="ts">
import { computed, ref } from 'vue'
import type { MapNode } from '../api'

const props = defineProps<{ node: MapNode; depth: number; heat: boolean; maxReads: number; expandAll: boolean }>()
const open = ref(props.expandAll || props.depth === 0)

const total = computed(() => Math.max(1, props.node.tiers.verified + props.node.tiers.provisional + props.node.tiers.stale))
// A conic gradient shows the tier mix of the subtree as one disc.
const disc = computed(() => {
  const t = props.node.tiers
  if (props.node.entries === 0) return 'var(--line)'
  const a = (t.verified / total.value) * 100
  const b = a + (t.provisional / total.value) * 100
  return `conic-gradient(var(--verified) 0 ${a}%, var(--provisional) ${a}% ${b}%, var(--stale) ${b}% 100%)`
})
const size = computed(() => {
  if (!props.heat) return 16
  return 12 + Math.round(26 * Math.sqrt(props.node.reads_30d / Math.max(1, props.maxReads)))
})
const expandable = computed(() => props.node.children.length > 0 || props.node.matches.length > 0)
const mix = computed(() => {
  const t = props.node.tiers
  return `${t.verified} verified, ${t.provisional} provisional, ${t.stale} stale`
})
</script>

<template>
  <li class="node">
    <div class="node-row" :style="{ minHeight: heat ? `${size + 12}px` : undefined }">
      <button v-if="expandable" class="twist" :aria-expanded="open" :aria-label="`${open ? 'Collapse' : 'Expand'} ${node.title}`" @click="open = !open">
        <span :class="{ turned: open }" aria-hidden="true">&#9656;</span>
      </button>
      <span v-else class="twist-gap"></span>
      <span class="disc" :title="mix" :style="{ width: `${size}px`, height: `${size}px`, background: disc }"></span>
      <RouterLink class="node-title" :to="{ name: 'sign', params: { id: node.id } }">{{ node.title }}</RouterLink>
      <span class="badges">
        <span class="count" :title="mix">{{ node.entries }} {{ node.entries === 1 ? 'entry' : 'entries' }}</span>
        <span v-if="node.stale" class="pill pill-stale" title="Stale entries below">{{ node.stale }} stale</span>
        <span v-if="node.open_gaps" class="pill pill-gap" title="Open gaps at or below">{{ node.open_gaps }} {{ node.open_gaps === 1 ? 'gap' : 'gaps' }}</span>
        <span v-if="heat" class="count">{{ node.reads_30d }} reads</span>
      </span>
    </div>
    <ul v-if="open && expandable" class="tree">
      <li v-for="m in node.matches" :key="m.id" class="leaf">
        <span class="disc small-disc" :class="`bg-${m.tier}`"></span>
        <RouterLink :to="{ name: 'entry', params: { id: m.id } }">{{ m.title }}</RouterLink>
        <span class="muted small">{{ m.kind }}</span>
      </li>
      <MapNode v-for="c in node.children" :key="c.id" :node="c" :depth="depth + 1" :heat="heat" :max-reads="maxReads" :expand-all="expandAll" />
    </ul>
  </li>
</template>
