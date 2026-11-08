<script setup lang="ts">
import AuthForm from './AuthForm.vue'
import type { AuthState } from '../../auth/state'
import type { User } from '../../types/api'

defineProps<{ auth?: AuthState }>()
const emit = defineEmits<{ registered: [user: User], 'show-login': [] }>()
</script>

<template>
  <AuthForm mode="register" :auth="auth" @registered="emit('registered', $event)" @switch-mode="emit('show-login')" />
</template>
