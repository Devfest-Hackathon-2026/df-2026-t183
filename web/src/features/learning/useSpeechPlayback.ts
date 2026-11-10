import { useEffect, useRef, useState } from 'react'
import { synthesizeSpeech } from '../../lib/api/learning'

type Phase = 'idle' | 'loading' | 'ready' | 'playing'

export function useSpeechPlayback(question: string, turn: number, active: boolean) {
  const [phase, setPhase] = useState<Phase>('idle')
  const [error, setError] = useState('')
  const request = useRef<AbortController | null>(null)
  const player = useRef<HTMLAudioElement | null>(null)
  const objectURL = useRef('')
  const version = useRef(0)
  const attemptedTurn = useRef(0)

  function stop() {
    dispose()
    setPhase('idle')
    setError('')
  }

  function dispose() {
    version.current++
    request.current?.abort()
    request.current = null
    if (player.current) {
      player.current.onplay = null
      player.current.onpause = null
      player.current.onended = null
      player.current.pause()
      player.current.removeAttribute('src')
      player.current.load()
      player.current = null
    }
    if (objectURL.current) {
      URL.revokeObjectURL(objectURL.current)
      objectURL.current = ''
    }
  }

  async function playAudio(audio: HTMLAudioElement, current: number, automatic: boolean) {
    setError('')
    audio.currentTime = 0
    try {
      await audio.play()
      if (version.current !== current) audio.pause()
    } catch {
      if (version.current === current) {
        setPhase('ready')
        setError(automatic
          ? '自動再生できませんでした。再生ボタンを押してください。'
          : '音声を再生できませんでした。文字で質問を確認してください。')
      }
    }
  }

  async function loadAndPlay() {
    if (!active || !question || request.current || player.current) return
    const current = ++version.current
    const controller = new AbortController()
    request.current = controller
    setPhase('loading')
    setError('')
    try {
      const blob = await synthesizeSpeech(question, controller.signal)
      if (version.current !== current || controller.signal.aborted) return
      const url = URL.createObjectURL(blob)
      objectURL.current = url
      const audio = new Audio(url)
      audio.onplay = () => { if (version.current === current && player.current === audio && !audio.paused) setPhase('playing') }
      audio.onpause = () => { if (version.current === current && player.current === audio) setPhase('ready') }
      audio.onended = () => { if (version.current === current && player.current === audio) setPhase('ready') }
      player.current = audio
      setPhase('ready')
      await playAudio(audio, current, true)
    } catch (cause) {
      if (version.current !== current || controller.signal.aborted) return
      dispose()
      setPhase('idle')
      setError(cause instanceof Error ? cause.message : '読み上げに失敗しました。文字で質問を確認してください。')
    } finally {
      if (request.current === controller) request.current = null
    }
  }

  function play() {
    if (!active || !question) return
    if (player.current) {
      if (phase === 'playing') return
      void playAudio(player.current, version.current, false)
    } else {
      void loadAndPlay()
    }
  }

  useEffect(() => {
    if (!active) {
      dispose()
      setPhase('idle')
      setError('')
      return
    }
    if (!question || !turn || attemptedTurn.current === turn) return
    attemptedTurn.current = turn
    dispose()
    void loadAndPlay()
  }, [active, question, turn])

  useEffect(() => () => dispose(), [])

  return { phase, error, play, stop }
}
