import { onUnmounted, ref } from 'vue'

interface Recognition {
  lang: string
  continuous: boolean
  interimResults: boolean
  onresult: ((event: { resultIndex: number; results: ArrayLike<{ isFinal: boolean; 0: { transcript: string } }> }) => void) | null
  onerror: ((event: { error: string }) => void) | null
  onend: (() => void) | null
  start(): void
  stop(): void
  abort(): void
}

type SpeechWindow = Window & {
  SpeechRecognition?: new () => Recognition
  webkitSpeechRecognition?: new () => Recognition
}

export function useArenaVoice(append: (text: string) => void) {
  const listening = ref(false)
  const voiceNotice = ref('')
  let recognition: Recognition | undefined

  function stopVoice() { recognition?.stop() }

  function toggleVoice() {
    if (listening.value) { stopVoice(); return }
    const browser = window as SpeechWindow
    const Recognize = browser.SpeechRecognition ?? browser.webkitSpeechRecognition
    if (!Recognize || !window.isSecureContext) {
      voiceNotice.value = '当前浏览器或页面不支持语音输入，可以继续打字。'
      return
    }
    voiceNotice.value = ''
    const current = new Recognize()
    recognition = current
    current.lang = 'zh-CN'
    current.continuous = true
    current.interimResults = false
    current.onresult = event => {
      if (recognition !== current) return
      for (let index = event.resultIndex; index < event.results.length; index++) {
        const result = event.results[index]
        if (result?.isFinal && result[0].transcript.trim()) append(result[0].transcript.trim())
      }
    }
    current.onerror = ({ error }) => {
      if (recognition !== current) return
      voiceNotice.value = error === 'not-allowed' || error === 'service-not-allowed'
        ? '麦克风权限未开启，可以在浏览器中授权，或继续打字。'
        : '语音识别没有完成，请重试或继续打字。'
      listening.value = false
      recognition = undefined
    }
    current.onend = () => {
      if (recognition !== current) return
      listening.value = false
      recognition = undefined
    }
    try { current.start(); listening.value = true } catch {
      current.onerror({ error: 'start' })
    }
  }

  onUnmounted(() => {
    const current = recognition
    recognition = undefined
    current?.abort()
  })

  return { listening, voiceNotice, toggleVoice, stopVoice }
}
