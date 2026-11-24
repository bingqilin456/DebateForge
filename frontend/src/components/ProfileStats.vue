<script setup lang="ts">
import type { PublicProfile } from '../api/profile'
defineProps<{ profile: PublicProfile }>()
</script>

<template>
  <dl class="profile-stats">
    <div><dt>段位</dt><dd>{{ profile.rank_label }}</dd></div>
    <div><dt>ELO</dt><dd>{{ profile.elo }}</dd></div>
    <div><dt>胜率</dt><dd>{{ profile.win_rate.toFixed(1) }}%</dd></div>
    <div><dt>最高杯赛阶段</dt><dd>{{ profile.cup_stage }}</dd></div>
  </dl>
</template>

<style scoped>
.profile-stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(115px, 1fr)); gap: 12px; margin: 18px 0 0; }
.profile-stats div { padding: 12px; border: 1px solid var(--app-border); border-radius: var(--app-radius-control); }
dt { color: var(--app-text-muted); font-size: 13px; }
dd { margin: 6px 0 0; font-weight: 700; }
</style>
