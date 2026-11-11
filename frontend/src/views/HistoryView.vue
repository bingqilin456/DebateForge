<script setup lang="ts">
import { Button } from '@arco-design/web-vue'
import { authState, type AuthState } from '../auth/state'

// T11（本人历史与详情查询）占位组件，具体 UI 由成员 B 实现。
// 可直接调用 ../api/debate 中的 listDebates(page, pageSize, auth.client)、getDebateDetail(id, auth.client)，
// 以及 ../types/api 中的 Debate、DebateListResult、DebateDetail 类型。
export interface HistoryViewProps {
  auth?: AuthState
  pageSize?: number
  disabled?: boolean
}

const props = withDefaults(defineProps<HistoryViewProps>(), {
  pageSize: 20,
  disabled: false,
})

const emit = defineEmits<{
  /** 选中某条历史会话后触发，交由顶层切换至辩论主视图重读详情或继续未完成会话 */
  'select-debate': [debateId: string]
  /** 按会话 ID 打开辩论主视图（与 select-debate 等价） */
  'open-debate': [debateId: string]
  /** 切换至辩题列表视图（T08） */
  'open-topics': []
  /** 返回当前辩论主视图 */
  'back-to-debate': []
}>()

const currentAuth = props.auth ?? authState
</script>

<template>
  <section class="card history-view-placeholder" aria-label="历史会话视图" data-view="history">
    <div class="view-header">
      <h2>本人历史会话</h2>
      <span class="muted">
        T11 页面占位 · 默认每页 {{ pageSize }} 条 · 当前用户：{{ currentAuth.state.user?.username ?? '未确认' }}
      </span>
    </div>
    <p class="muted">
      本视图预留给成员 B 实现本人历史分页列表、空状态与完成/进行中状态展示（调用
      <code>listDebates(page, pageSize)</code> 与 <code>getDebateDetail(id)</code>，点击记录触发
      <code>select-debate</code> 或 <code>open-debate</code> 事件进入辩论主视图）。
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
        data-nav-topics
        :disabled="disabled || currentAuth.state.pending"
        @click="emit('open-topics')"
      >
        前往辩题列表
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
