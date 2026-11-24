<script setup lang="ts">
import type { ProfileIdentity } from '../api/profile'
defineProps<{ profile: ProfileIdentity; size?: 'small' | 'large' }>()
</script>

<template>
  <span class="portrait" :class="size">
    <img v-if="profile.avatar_kind === 'image'" :src="profile.avatar_image" alt="" />
    <span v-else aria-hidden="true">{{ profile.avatar_seal }}</span>
  </span>
</template>

<style scoped>
.portrait { width: 50px; height: 50px; display: inline-grid; place-items: center; flex: none; overflow: hidden; border-radius: 10px; background: var(--app-pro); color: #fff; font: 700 24px var(--app-serif); vertical-align: middle; }
.portrait.small { width: 30px; height: 30px; border-radius: 7px; font-size: 16px; }
.portrait.large { width: 64px; height: 64px; font-size: 32px; }
.portrait img { width: 100%; height: 100%; object-fit: cover; }
</style>
