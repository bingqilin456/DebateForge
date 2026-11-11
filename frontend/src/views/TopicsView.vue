<script setup lang="ts">
import { Button } from '@arco-design/web-vue'
import { authState, type AuthState } from '../auth/state'
import type { Debate } from '../types/api'

// T08（辩题列表与立场选择）占位组件，具体 UI 由成员 B 实现。
// 可直接调用 ../api/debate 中的 listTopics(auth.client)、createDebate(topicId, userStance, auth.client)，
// 以及 ../types/api 中的 Topic、Stance、CreateDebateRequest、Debate 类型。
export interface TopicsViewProps {
  auth?: AuthState
  disabled?: boolean
}

const props = withDefaults(defineProps<TopicsViewProps>(), {
  disabled: false,
})

const emit = defineEmits<{
  /** 创建辩论会话成功后触发，交由顶层视图切换至辩论主视图并加载该会话 */
  created: [debate: Debate]
  /** 按会话 ID 切换至辩论主视图并打开对应会话 */
  'open-debate': [debateId: string]
  /** 切换至本人历史会话视图（T11） */
  'open-history': []
  /** 返回当前辩论主视图 */
  'back-to-debate': []
}>()

const currentAuth = props.auth ?? authState
</script>

<template>
  <section class="card topics-view-placeholder" aria-label="辩题列表视图" data-view="topics">
    <div class="view-header">
      <h2>辩题列表与立场选择</h2>
      <span class="muted">T08 页面占位 · 当前用户：{{ currentAuth.state.user?.username ?? '未确认' }}</span>
    </div>
    <p class="muted">
      本视图预留给成员 B 实现辩题卡片列表、正反方立场选择与创建会话交互（调用
      <code>listTopics</code> 与 <code>createDebate</code>，创建成功后触发
      <code>created</code> 或 <code>open-debate</code> 事件进入辩论主视图）。
    </p>
    <div class="placeholder-actions">
      <Button
        type="primary"
        data-nav-debate
        :disabled="disabled || currentAuth.state.pending"
        @click="emit('back-to-debate')"
      >
        返回辩论主视图
      </Button>
      <Button
        type="outline"
        data-nav-history
        :disabled="disabled || currentAuth.state.pending"
        @click="emit('open-history')"
      >
        查看历史会话
      </Button>
    </div>
  </section>
</template>

<style scoped>
.view-header {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 12px;
}
.view-header h2 {
  margin-bottom: 0;
}
.muted {
  color: #526578;
  font-size: 13px;
}
.placeholder-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 20px;
}
</style>
