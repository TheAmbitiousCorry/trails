<script setup lang="ts">
defineProps<{ ready: boolean; loading: boolean; error: string }>()
</script>

<template>
  <p v-if="error" class="notice error" role="alert">{{ error }}</p>
  <p v-else-if="!ready && loading" class="muted">Loading</p>
  <slot v-else-if="ready" />
</template>
