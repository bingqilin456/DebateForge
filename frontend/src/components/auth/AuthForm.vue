<script setup lang="ts">
import { computed, onUnmounted, ref, useId } from 'vue'
import { Alert, Button, Input } from '@arco-design/web-vue'
import { ApiError } from '../../api/client'
import { authState, type AuthState } from '../../auth/state'
import { normalizeCredentials, validateCredentials } from '../../auth/credentials'
import type { User } from '../../types/api'

const props = defineProps<{ mode: 'login' | 'register', auth?: AuthState }>()
const emit = defineEmits<{
  authenticated: [user: User]
  registered: [user: User]
  'switch-mode': []
}>()
const auth = props.auth ?? authState
const id = useId()
const username = ref('')
const password = ref('')
const submitting = ref(false)
const errorMessage = ref('')
const successMessage = ref('')
const busy = computed(() => submitting.value || auth.state.pending)
const isLogin = computed(() => props.mode === 'login')
let active = true

onUnmounted(() => {
  active = false
  password.value = ''
})

async function submit() {
  // 同步置位，防止 DOM 尚未更新 disabled 时连续 Enter/点击发出两个请求。
  if (busy.value) return
  errorMessage.value = ''
  successMessage.value = ''
  const input = { username: username.value, password: password.value }
  const error = validateCredentials(input)
  if (error) { errorMessage.value = error; return }
  submitting.value = true
  try {
    const credentials = normalizeCredentials(input)
    if (isLogin.value) {
      const user = await auth.login(credentials)
      if (active) { successMessage.value = '登录成功。'; emit('authenticated', user) }
    } else {
      const user = await auth.register(credentials)
      if (active) { successMessage.value = '注册成功，请登录。'; emit('registered', user) }
    }
  } catch (error) {
    if (active) errorMessage.value = error instanceof ApiError ? error.message : '操作失败，请稍后手动重试。'
  } finally {
    // 请求结束无论成功失败都清空组件密码；只向父组件发送公开 User。
    password.value = ''
    submitting.value = false
  }
}
</script>

<template>
  <section class="auth-card" :aria-labelledby="`${id}-heading`">
    <h2 :id="`${id}-heading`">{{ isLogin ? '登录思弈阁' : '创建思弈阁账号' }}</h2>
    <p class="auth-intro">{{ isLogin ? '使用用户名与密码继续练习。' : '只需用户名与密码，不收集联系方式。' }}</p>
    <form novalidate :aria-busy="busy" @submit.prevent="submit">
      <div class="auth-field">
        <label :for="`${id}-username`">用户名</label>
        <Input v-model="username" :disabled="busy" size="large" :input-attrs="{
          id: `${id}-username`, name: 'username', autocomplete: 'username',
          'aria-describedby': `${id}-username-help`, 'aria-invalid': Boolean(errorMessage),
        }" />
        <p :id="`${id}-username-help`" class="auth-help">3–32 位 ASCII 字母、数字或下划线，忽略大小写与首尾空白。</p>
      </div>
      <div class="auth-field">
        <label :for="`${id}-password`">密码</label>
        <Input v-model="password" type="password" :disabled="busy" size="large" :input-attrs="{
          id: `${id}-password`, name: 'password', autocomplete: isLogin ? 'current-password' : 'new-password',
          'aria-describedby': `${id}-password-help`, 'aria-invalid': Boolean(errorMessage),
        }" />
        <p :id="`${id}-password-help`" class="auth-help">12–128 个 Unicode 字符，最多 512 字节；保留首尾空白。</p>
      </div>
      <Alert v-if="errorMessage" class="auth-feedback" type="error" :title="errorMessage" role="alert" />
      <Alert v-if="successMessage" class="auth-feedback" type="success" :title="successMessage" role="status" />
      <Button type="primary" html-type="submit" size="large" long :loading="busy" :disabled="busy">
        {{ busy ? '正在处理…' : isLogin ? '登录' : '注册' }}
      </Button>
      <Button class="auth-switch" type="text" html-type="button" :disabled="busy" @click="emit('switch-mode')">
        {{ isLogin ? '还没有账号？去注册' : '已有账号？去登录' }}
      </Button>
    </form>
  </section>
</template>

<style scoped>
.auth-card { width: 100%; max-width: 440px; padding: 28px; border: 1px solid #e3e8ee; border-radius: 16px; background: #fff; color: #172b3a; }
.auth-card h2 { margin: 0 0 8px; font-size: 22px; }
.auth-intro { margin: 0 0 24px; color: #526578; line-height: 1.6; }
.auth-field { margin-bottom: 20px; }
.auth-field label { display: block; margin-bottom: 8px; font-weight: 600; }
.auth-help { margin: 8px 0 0; color: #526578; font-size: 12px; line-height: 1.7; }
.auth-feedback { margin-bottom: 20px; }
.auth-switch { display: block; margin: 12px auto 0; }
@media (max-width: 480px) { .auth-card { padding: 22px 18px; } }
</style>
