<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { Button } from '@arco-design/web-vue'
import { ApiError, type ApiClient } from '../api/client'
import { createPersonalModelApi } from '../api/personalModel'
import { createJuryApi, type JuryModel } from '../api/jury'
import type { PersonalModelProvider, PersonalModelStatus } from '../types/api'

const props = defineProps<{ client: ApiClient }>()
const api = createPersonalModelApi(props.client)
const juryApi = createJuryApi(props.client, '')
const providerNames: Record<PersonalModelProvider, string> = {
  deepseek: 'DeepSeek', openai: 'OpenAI GPT', doubao: '豆包（火山方舟）', custom: '自定义 OpenAI 兼容服务',
}
const status = ref<PersonalModelStatus>()
const provider = ref<PersonalModelProvider>('deepseek')
const model = ref('')
const alias = ref('')
const baseUrl = ref('')
const key = ref('')
const pending = ref<'load' | 'save' | 'test' | 'remove' | 'save-judge' | 'remove-judge' | null>(null)
const judgeModels = ref<JuryModel[]>([])
const notice = ref('')
const error = ref('')
const confirmRemove = ref(false)
let active = true

const canSave = computed(() => status.value?.enabled && model.value.trim() && alias.value.trim() && key.value.trim().length >= 16
  && (provider.value !== 'custom' || baseUrl.value.trim().startsWith('https://')))

async function run(action: NonNullable<typeof pending.value>, operation: () => Promise<void>) {
  if (pending.value) return
  pending.value = action
  error.value = ''
  notice.value = ''
  try { await operation() }
  catch (cause) {
    if (active) error.value = cause instanceof ApiError ? cause.message : '操作未完成，请手动重试。'
  } finally { pending.value = null }
}

function load() {
  return run('load', async () => {
    status.value = await api.get()
    if (status.value.enabled) void getJudgeModels()
    if (!status.value.configured) return
    provider.value = status.value.provider ?? 'deepseek'
    model.value = status.value.model ?? ''
    alias.value = status.value.alias ?? ''
    baseUrl.value = status.value.base_url ?? ''
  })
}

async function getJudgeModels() {
  try {
    const { items } = await juryApi.models()
    if (active) judgeModels.value = items.filter(item => /^[1-9]\d*$/.test(item.id))
  } catch (cause) {
    if (active && cause instanceof ApiError && cause.status !== 404) error.value = cause.message
  }
}

function save(saveForJudge = false) {
  if (!canSave.value) return
  return run(saveForJudge ? 'save-judge' : 'save', async () => {
    const secret = key.value.trim()
    key.value = ''
    const config = { provider: provider.value, model: model.value.trim(), alias: alias.value.trim(),
      ...(provider.value === 'custom' ? { base_url: baseUrl.value.trim() } : {}), api_key: secret }
    if (saveForJudge) {
      await juryApi.addModel(config)
      await getJudgeModels()
      notice.value = '裁判模型已保存，可以按别名选入裁判席。'
      return
    }
    status.value = await api.save(config)
    confirmRemove.value = false
    notice.value = '配置已保存，下一次生成将使用此配置。'
  })
}

function removeJudgeModel(id: string) {
  return run('remove-judge', async () => {
    await juryApi.removeModel(id)
    await getJudgeModels()
    notice.value = '裁判模型已移除。'
  })
}

function testConnection() {
  if (!status.value?.configured) return
  return run('test', async () => {
    await api.test()
    status.value = await api.get()
    notice.value = '已通过连接测试。'
  })
}

function remove() {
  if (!status.value?.configured || !confirmRemove.value) return
  return run('remove', async () => {
    await api.remove()
    status.value = { enabled: true, configured: false }
    key.value = ''
    confirmRemove.value = false
    notice.value = '个人模型已停用。'
  })
}

onMounted(load)
onUnmounted(() => { active = false; key.value = '' })
</script>

<template>
  <section class="model-settings" aria-label="模型连接设置" :aria-busy="pending !== null">
    <p v-if="pending === 'load'" role="status">正在读取模型配置…</p>
    <template v-if="status?.enabled">
      <div class="model-status">
        <strong>{{ status.configured ? status.alias || '个人模型' : '本地演示' }}</strong>
        <ul v-if="status.configured" aria-label="配置状态">
          <li>已保存</li>
          <li>{{ status.verified ? '连接已验证' : '连接待验证' }}</li>
          <li>{{ status.used ? '已实际调用' : '尚无实际调用' }}</li>
        </ul>
      </div>
      <form @submit.prevent="save()">
        <fieldset class="model-fields" :disabled="pending !== null">
          <label for="neon-provider">服务商</label>
          <select id="neon-provider" v-model="provider">
            <option v-for="(name, value) in providerNames" :key="value" :value="value">{{ name }}</option>
          </select>
          <label for="neon-model-id">模型 ID</label>
          <input id="neon-model-id" v-model="model" maxlength="128" autocomplete="off" required />
          <label for="neon-model-alias">显示别名</label>
          <input id="neon-model-alias" v-model="alias" maxlength="128" autocomplete="off" required />
          <template v-if="provider === 'custom'">
            <label for="neon-model-url">API 地址</label>
            <input id="neon-model-url" v-model="baseUrl" type="url" placeholder="https://你的服务/v1" maxlength="512" required />
          </template>
          <label for="neon-model-key">API Key</label>
          <input id="neon-model-key" v-model="key" type="password" minlength="16" maxlength="256" autocomplete="new-password" required />
        </fieldset>
        <p class="hint">密钥由服务端加密保存。「测试连接」会向所选服务发出一次请求。</p>
        <div class="model-actions">
          <Button html-type="submit" type="primary" :loading="pending === 'save'" :disabled="!canSave || pending !== null">保存配置</Button>
          <Button :loading="pending === 'save-judge'" :disabled="!canSave || pending !== null" @click="save(true)">仅保存为裁判模型</Button>
          <Button :disabled="!status.configured || pending !== null" :loading="pending === 'test'" @click="testConnection">测试连接</Button>
          <Button v-if="status.configured" :disabled="pending !== null" @click="confirmRemove = !confirmRemove">停用个人模型</Button>
        </div>
      </form>
      <section v-if="judgeModels.length" class="judge-model-list" aria-label="已保存的裁判模型">
        <h3>已保存的裁判模型</h3>
        <div v-for="item in judgeModels" :key="item.id" class="model-actions">
          <span>{{ item.alias }}</span>
          <Button type="text" :aria-label="`移除裁判模型 ${item.alias}`" :disabled="pending !== null" @click="removeJudgeModel(item.id)">移除</Button>
        </div>
      </section>
      <div v-if="confirmRemove" class="remove-confirm" role="group" aria-label="确认停用">
        <p>停用会移除已保存的模型配置，之后可重新录入。</p>
        <Button status="danger" :disabled="pending !== null" @click="remove">确认停用</Button>
        <Button :disabled="pending !== null" @click="confirmRemove = false">返回</Button>
      </div>
    </template>
    <p v-else-if="status">当前环境使用本地演示，个人模型设置暂未开放。</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <p v-if="error" class="model-error" role="alert">{{ error }}</p>
    <Button v-if="error && !status" @click="load">重新读取</Button>
  </section>
</template>

<style scoped>
.model-status, .remove-confirm { padding: 12px 16px; background: var(--app-background); border-radius: 10px; }
.model-status ul { display: flex; flex-wrap: wrap; gap: 6px 15px; padding: 0; margin: 8px 0 0; list-style: none; color: var(--app-text-muted); }
.model-fields { display: grid; align-items: center; grid-template-columns: 6em minmax(0, 1fr); gap: 10px; margin: 16px 0 8px; padding: 0; border: 0; min-width: 0; }
.model-fields label { font-weight: 650; }
.model-fields input, .model-fields select { width: 100%; min-height: 44px; padding: 8px 10px; border: 1px solid var(--app-input-border); border-radius: 8px; background: var(--app-surface); color: var(--app-text); font: inherit; }
.model-actions { display: flex; flex-wrap: wrap; gap: 8px; }
.judge-model-list { margin-top: 15px; }
.judge-model-list .model-actions { align-items: center; justify-content: space-between; overflow-wrap: anywhere; }
.hint { color: var(--app-text-muted); font-size: 13px; }
.remove-confirm { margin-top: 12px; }
.remove-confirm button { margin-inline-end: 8px; }
.model-error { color: var(--app-error-text); overflow-wrap: anywhere; }
@media (max-width: 480px) { .model-fields { grid-template-columns: minmax(0, 1fr); gap: 6px; } }
</style>
