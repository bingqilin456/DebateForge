<script setup lang="ts">
import AuthForm from './AuthForm.vue'
import type { AuthState } from '../../auth/state'
import type { User } from '../../types/api'

defineProps<{ auth?: AuthState }>()
const emit = defineEmits<{ authenticated: [user: User], 'show-register': [] }>()
</script>

<template>
  <AuthForm mode="login" :auth="auth" @authenticated="emit('authenticated', $event)" @switch-mode="emit('show-register')" />
</template>
