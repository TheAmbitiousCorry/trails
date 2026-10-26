<script setup lang="ts">
defineProps<{ tier: string }>()
</script>

<template>
  <span class="tier" :class="`tier-${tier}`"><span class="dot" aria-hidden="true"></span>{{ tier }}</span>
</template>
