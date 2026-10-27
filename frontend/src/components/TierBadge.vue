<script setup lang="ts">
import { tip } from '../tips'
defineProps<{ tier: string; dotOnly?: boolean }>()
</script>

<template>
  <span class="tier" :class="`tier-${tier}`" v-bind="tip(tier)" :aria-label="dotOnly ? tier : undefined"><span class="dot" aria-hidden="true"></span><template v-if="!dotOnly">{{ tier }}</template></span>
</template>
