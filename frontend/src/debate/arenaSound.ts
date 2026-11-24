import { ref } from 'vue'

const preferenceKey = 'siyige-arena-sound'
function storedPreference() {
  try { return window.localStorage.getItem(preferenceKey) === 'on' } catch { return false }
}

export const arenaSoundEnabled = ref(storedPreference())
let audio: AudioContext | undefined

export function toggleArenaSound() {
  arenaSoundEnabled.value = !arenaSoundEnabled.value
  try { window.localStorage.setItem(preferenceKey, arenaSoundEnabled.value ? 'on' : 'off') } catch { /* 私密模式下只保留本次偏好 */ }
  playArenaSound()
}

export function playArenaSound(frequency = 480) {
  if (!arenaSoundEnabled.value || !window.AudioContext) return
  try {
    audio ??= new AudioContext()
    void audio.resume().then(() => {
      if (!arenaSoundEnabled.value || !audio) return
      const tone = audio.createOscillator()
      const gain = audio.createGain()
      tone.frequency.value = frequency
      gain.gain.setValueAtTime(0.045, audio.currentTime)
      gain.gain.exponentialRampToValueAtTime(0.001, audio.currentTime + 0.12)
      tone.connect(gain).connect(audio.destination)
      tone.start()
      tone.stop(audio.currentTime + 0.12)
    }).catch(() => {})
  } catch { /* 浏览器音频故障不影响发言 */ }
}
