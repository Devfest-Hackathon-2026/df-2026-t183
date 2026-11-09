import { useEffect, useRef, useState } from 'react'
import { FILE_LIMIT_MIB, MIB } from './limits'

type Session = {
  stream?: MediaStream
  recorder?: MediaRecorder
  chunks: Blob[]
  timer?: number
  clock?: number
  stopping: boolean
}

export function supportedRecordingType() {
  if (typeof MediaRecorder === 'undefined' || !navigator.mediaDevices?.getUserMedia) return ''
  return ['audio/webm;codecs=opus', 'audio/ogg;codecs=opus', 'audio/mp4;codecs=mp4a.40.2']
    .find((type) => MediaRecorder.isTypeSupported(type)) ?? ''
}

export function useRecording(onTranscribe: (audio: Blob, type: string, done: (text: string) => void) => void) {
  const [phase, setPhase] = useState<'idle' | 'requesting' | 'recording' | 'stopping'>('idle')
  const [seconds, setSeconds] = useState(0)
  const [error, setError] = useState('')
  const [recognized, setRecognized] = useState('')
  const active = useRef<Session | null>(null)
  const generation = useRef(0)
  const mounted = useRef(true)
  const mediaType = supportedRecordingType()

  function release(session: Session) {
    window.clearTimeout(session.timer)
    window.clearInterval(session.clock)
    session.stream?.getTracks().forEach((track) => track.stop())
    session.stream = undefined
  }

  function cancel() {
    generation.current++
    const session = active.current
    active.current = null
    if (session) {
      session.chunks = []
      release(session)
      if (session.recorder?.state === 'recording') session.recorder.stop()
    }
    if (mounted.current) {
      setPhase('idle')
      setSeconds(0)
      setRecognized('')
    }
  }

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      cancel()
    }
  }, [])

  function stop() {
    const session = active.current
    if (!session || session.stopping || !session.recorder) return
    session.stopping = true
    window.clearTimeout(session.timer)
    window.clearInterval(session.clock)
    setPhase('stopping')
    if (session.recorder.state !== 'inactive') session.recorder.stop()
    release(session)
  }

  async function start() {
    if (active.current || !mounted.current || !mediaType) return
    const session: Session = { chunks: [], stopping: false }
    active.current = session
    const token = ++generation.current
    const current = () => mounted.current && active.current === session
    setPhase('requesting')
    setError('')
    setRecognized('')
    setSeconds(0)
    let stream: MediaStream
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true })
    } catch {
      if (current()) {
        active.current = null
        setPhase('idle')
        setError('マイクを利用できません。許可を確認するか、文字で入力してください。')
      }
      return
    }
    if (!current()) {
      stream.getTracks().forEach((track) => track.stop())
      return
    }
    session.stream = stream
    try {
      const recorder = new MediaRecorder(stream, { mimeType: mediaType })
      session.recorder = recorder
      recorder.ondataavailable = (event) => {
        if (current() && event.data.size) session.chunks.push(event.data)
      }
      recorder.onerror = () => {
        if (!current()) return
        cancel()
        setError('録音に失敗しました。もう一度お試しください。')
      }
      recorder.onstop = () => {
        release(session)
        if (!current()) { session.chunks = []; return }
        active.current = null
        setPhase('idle')
        const audio = new Blob(session.chunks, { type: recorder.mimeType || mediaType })
        session.chunks = []
        if (!audio.size || audio.size > FILE_LIMIT_MIB * MIB) {
          setError(audio.size ? '録音が5 MiBを超えました。短く録音してください。' : '音声を録音できませんでした。もう一度お試しください。')
          return
        }
        onTranscribe(audio, audio.type, (text) => {
          if (mounted.current && generation.current === token) setRecognized(text)
        })
      }
      recorder.start()
      setPhase('recording')
      const startedAt = Date.now()
      session.clock = window.setInterval(() => {
        if (current()) setSeconds(Math.min(30, Math.floor((Date.now() - startedAt) / 1000)))
      }, 250)
      session.timer = window.setTimeout(() => { if (current()) stop() }, 30_000)
    } catch {
      if (current()) {
        cancel()
        setError('この録音形式には対応していません。文字で入力してください。')
      } else release(session)
    }
  }

  return { phase, seconds, error, recognized, setRecognized, mediaType, start, stop, cancel }
}
