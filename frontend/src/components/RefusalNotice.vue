<script setup lang="ts">
import { ref } from 'vue'
import type { Refusal } from '../refusal'

defineProps<{ value: Refusal }>()
const root = ref<HTMLElement | null>(null)
// A refusal is announced by role=alert; focusing it also moves a keyboard user to the first thing to read.
defineExpose({ focus: () => root.value?.focus() })
</script>

<template>
  <div ref="root" class="refusal" role="alert" tabindex="-1">
    <template v-if="'rule' in value"><strong>{{ value.rule }}</strong>: {{ value.fix }}</template>
    <template v-else>{{ value.message }}</template>
  </div>
</template>
