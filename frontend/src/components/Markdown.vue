<script setup lang="ts">
import { computed } from 'vue'
import { renderMarkdown } from '../markdown'

const props = defineProps<{ source: string }>()
const html = computed(() => renderMarkdown(props.source))
</script>

<template>
  <div class="prose" v-html="html"></div>
</template>
