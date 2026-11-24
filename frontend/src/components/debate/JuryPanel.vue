<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Button } from '@arco-design/web-vue'
import { ApiError, type ApiClient } from '../../api/client'
import { createJuryApi, type Judge, type JuryModel, type SeatChoice, type Verdict } from '../../api/jury'

const props = defineProps<{ client: ApiClient; debateId: string; practice?: boolean }>()
const emit = defineEmits<{ settled: [] }>()
const api = createJuryApi(props.client, props.debateId)
const dimensions = ['逻辑严密性', '论据充分度', '临场反击力', '价值升华', '表达感染力']
const outcomes = { win: '我方胜出', loss: 'AI 胜出', draw: '平局' }
const judges = ref<Judge[]>([])
const models = ref<JuryModel[]>([])
const choices = ref<SeatChoice[]>([])
const verdict = ref<Verdict>()
const name = ref('')
const role = ref('')
const confirmPublish = ref<string>()
const busy = ref(false)
const notice = ref('')
const canEdit = computed(() => !verdict.value || verdict.value.status === 'not_started')
const availableModels = computed(() => props.practice ? models.value.filter(model => model.id === 'demo') : models.value)

async function work(action: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  notice.value = ''
  try { await action() }
  catch (error) { notice.value = error instanceof ApiError ? error.message : '裁判操作未完成，请查询结果后重试。' }
  finally { busy.value = false }
}
function refresh() {
  return work(async () => {
    const [panel, options, result] = await Promise.all([api.catalog(), api.models(), api.verdict()])
    judges.value = panel.items
    models.value = options.items
    applyVerdict(result)
    choices.value = choices.value.filter(choice => judges.value.some(judge => judge.id === choice.judge_id))
    for (const choice of choices.value) {
      if (!availableModels.value.some(model => model.id === choice.model_id)) choice.model_id = 'demo'
    }
  })
}
function toggle(judge: Judge) {
  if (choices.value.some(choice => choice.judge_id === judge.id)) {
    choices.value = choices.value.filter(choice => choice.judge_id !== judge.id)
  } else if (choices.value.length < 3) {
    choices.value.push({ judge_id: judge.id, model_id: 'demo' })
  }
}
function createJudge() {
  if (!name.value.trim() || !role.value.trim()) return
  return work(async () => {
    judges.value.push(await api.add(name.value.trim(), role.value.trim()))
    name.value = ''
    role.value = ''
  })
}
function changeVisibility(judge: Judge, visible: boolean) {
  return work(async () => {
    const saved = await api.visibility(judge, visible)
    judges.value = judges.value.map(item => item.id === saved.id ? saved : item)
    confirmPublish.value = undefined
  })
}
function send() {
  return work(async () => { applyVerdict(await api.judge(choices.value)) })
}
function retry(seat: string) {
  return work(async () => { applyVerdict(await api.retry(seat)) })
}
function applyVerdict(value: Verdict) {
  if (value.elo_delta != null && verdict.value?.elo_delta == null) emit('settled')
  verdict.value = value
}
onMounted(refresh)
</script>

<template>
  <section class="jury" aria-label="AI 裁判团" :aria-busy="busy">
    <header>
      <h3>AI 裁判团</h3>
      <Button size="small" :disabled="busy" @click="refresh">查询裁决状态</Button>
    </header>
    <p v-if="notice" role="alert">{{ notice }}</p>
    <template v-if="verdict">
      <details data-judge-pool>
        <summary>管理裁判池</summary>
        <form class="judge-form" @submit.prevent="createJudge">
          <label>裁判名称<input v-model="name" required maxlength="40" /></label>
          <label>评判风格<textarea v-model="role" required maxlength="300" rows="2" /></label>
          <Button html-type="submit" :disabled="busy">新增私密裁判</Button>
        </form>
        <ul class="pool">
          <li v-for="judge in judges" :key="judge.id">
            <strong>{{ judge.name }}</strong>
            <span>{{ judge.official ? '官方' : judge.public ? '公开' : '私密' }}</span>
            <p>{{ judge.role }}</p>
            <template v-if="judge.owned && !judge.official">
              <Button v-if="judge.public" size="small" :disabled="busy" @click="changeVisibility(judge, false)">撤回公开</Button>
              <template v-else-if="confirmPublish === judge.id">
                <Button size="small" :disabled="busy" @click="changeVisibility(judge, true)">确认公开裁判</Button>
                <Button size="small" @click="confirmPublish = undefined">取消公开</Button>
              </template>
              <Button v-else size="small" :disabled="busy" @click="confirmPublish = judge.id">公开裁判</Button>
            </template>
          </li>
        </ul>
      </details>
      <template v-if="canEdit">
        <p>选择 1～3 位裁判，每席可选已保存的模型别名或本地演示。</p>
        <fieldset :disabled="busy">
          <legend>裁判席位</legend>
          <label v-for="judge in judges" :key="judge.id" class="judge-choice">
            <input type="checkbox" :checked="choices.some(seat => seat.judge_id === judge.id)"
              :disabled="choices.length >= 3 && !choices.some(seat => seat.judge_id === judge.id)" @change="toggle(judge)" />
            {{ judge.name }}
          </label>
        </fieldset>
        <label v-for="(seat, i) in choices" :key="seat.judge_id" class="model-choice">
          第 {{ i + 1 }} 席模型
          <select v-model="seat.model_id" :disabled="busy">
            <option v-for="model in availableModels" :key="model.id" :value="model.id">{{ model.alias }}</option>
          </select>
        </label>
        <Button type="primary" :disabled="busy || !choices.length" @click="send">启动裁决</Button>
      </template>
      <p v-else-if="verdict.status === 'generating'" role="status">裁判正在评阅。可查询当前结果。</p>
      <div v-if="verdict.score" class="jury-score">
        <h4>{{ outcomes[verdict.score.outcome] }} <span>{{ verdict.score.user_total }} : {{ verdict.score.assistant_total }}</span></h4>
        <p>{{ verdict.score.source === 'ai' ? '模型裁决' : '本地演示评分' }}</p>
        <p v-if="verdict.elo_delta != null">ELO {{ verdict.elo_delta > 0 ? '+' : '' }}{{ verdict.elo_delta }}</p>
        <p v-else>本场暂无正式积分结算</p>
        <table aria-label="五维评分">
          <thead><tr><th>维度</th><th>我方</th><th>AI</th></tr></thead>
          <tbody><tr v-for="(dimension, i) in dimensions" :key="dimension">
            <th scope="row">{{ dimension }}</th>
            <td><meter min="0" max="100" :value="verdict.score.user[i] ?? 0" /> {{ verdict.score.user[i] }}</td>
            <td><meter min="0" max="100" :value="verdict.score.assistant[i] ?? 0" /> {{ verdict.score.assistant[i] }}</td>
          </tr></tbody>
        </table>
      </div>
      <ul v-if="verdict.seats.length" class="pool" aria-label="各席裁判意见">
        <li v-for="seat in verdict.seats" :key="seat.id" data-jury-seat>
          <strong>{{ seat.judge_name }}</strong><span>{{ seat.model_alias }}</span>
          <p v-if="seat.source === 'local' || seat.source === 'demo'">本地演示</p>
          <p v-else>个人模型</p>
          <p v-if="seat.reason">{{ seat.reason }}</p>
          <template v-if="seat.status === 'failed'">
            <p>此席裁决失败，其他席位结果仍可查看。</p>
            <Button size="small" :disabled="busy" @click="retry(seat.id)">重试此席</Button>
          </template>
        </li>
      </ul>
    </template>
  </section>
</template>

<style scoped>
.jury { margin-block: 22px; border: 1px solid var(--app-border); border-radius: var(--app-radius-panel); padding: 16px; }
header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; }
h3, h4 { margin: 0; font-family: var(--app-serif); }
details { margin-block: 12px; }
summary { cursor: pointer; }
.judge-form { display: grid; gap: 10px; margin-block: 10px; }
.judge-form input, .judge-form textarea, select { display: block; width: 100%; padding: 6px; border: 1px solid var(--app-input-border); border-radius: 8px; background: var(--app-surface); color: inherit; }
.pool { list-style: none; padding: 0; }
.pool li { padding-block: 8px; border-bottom: 1px solid var(--app-border); overflow-wrap: anywhere; }
.pool span { margin-inline-start: 10px; color: var(--app-text-muted); }
.pool p { margin-block: 4px; }
.judge-choice { display: inline-flex; align-items: center; gap: 5px; margin: 4px 10px 4px 0; }
.model-choice { display: block; margin-block: 8px; }
.jury-score { margin-block: 15px; }
.jury-score span { margin-inline-start: 8px; font-variant-numeric: tabular-nums; }
table { width: 100%; text-align: left; }
th, td { padding: 4px; }
meter { width: min(60%, 110px); }
</style>
