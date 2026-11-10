import { deadline } from '../abort.ts'
import { ANSWER_LIMIT, QUESTION_LIMIT, normalizeText, textLength } from '../../features/learning/limits.ts'

export type Turn = { role: 'model' | 'user'; content: string }
export type Summary = {
  understood: { point: string; evidence: string }[]
  review: { kind: 'misunderstanding' | 'unverified'; text: string }[]
}

export type Session = { sessionId: string; sessionProof: string; learningContent: string; history: Turn[] }
type LearningRequest = Omit<Session, 'sessionProof'> & { sessionProof?: string }
export type SavedResult = { sessionId: string; summary: Summary; saved: true; viewKey: string; savedAt: string; expiresAt: string }
export type LearningResult = SavedResult | { sessionId: string; summary: Summary; saved: false }
export type HistoryRecord = Omit<SavedResult, 'saved' | 'viewKey'> & { learningContent: string; history: Turn[] }
export const isSessionId = (value: unknown): value is string => typeof value === 'string' && /^[a-f\d]{8}-[a-f\d]{4}-4[a-f\d]{3}-[89ab][a-f\d]{3}-[a-f\d]{12}$/i.test(value)
export const isProof = (value: unknown): value is string => typeof value === 'string' && /^s1\.[A-Za-z0-9_-]{1,32}\.[0-9]{1,12}\.[A-Za-z0-9_-]{43}$/.test(value)
export const isDate = (value: unknown): value is string => typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$/.test(value) && Number.isFinite(Date.parse(value))
export function isHistory(value: unknown, requireModelEnd = true): value is Turn[] {
  return Array.isArray(value) && value.length >= 1 && value.length <= 41 && (!requireModelEnd || value.length % 2 === 1) && value.every((turn, i) => isRecord(turn) && turn.role === (i % 2 ? 'user' : 'model') && hasText(turn.content, i % 2 ? 1000 : 300))
}
function savedDates(value: Record<string, unknown>) {
  return isDate(value.savedAt) && isDate(value.expiresAt) && Date.parse(value.expiresAt) > Date.parse(value.savedAt)
}

const invalidResponse = 'ゼロの応答を確認できませんでした。もう一度お試しください。'

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function hasText(value: unknown, max: number, min = 1): value is string {
  if (typeof value !== 'string') return false
  const length = textLength(value)
  return length >= min && length <= max
}

function isReply(value: unknown): value is string {
  if (!hasText(value, QUESTION_LIMIT) || !/[？?]$/u.test(value)) return false
  const body = value.slice(0, -1)
  return textLength(body) > 0 && !/[?!？！。\n\r\v\f\u0085\u2028\u2029]/u.test(body) &&
    !/(^|[^a-zA-Z0-9])[.．]|[.．]($|[^a-zA-Z0-9])/u.test(body)
}

export function isSummary(value: unknown): value is Summary {
  return isRecord(value) &&
    Array.isArray(value.understood) && value.understood.length <= 3 &&
    value.understood.every((item: unknown) => isRecord(item) &&
      hasText(item.point, 80) && hasText(item.evidence, 200, 5)) &&
    Array.isArray(value.review) && value.review.length <= 3 &&
    value.review.every((item: unknown) => isRecord(item) &&
      (item.kind === 'misunderstanding' || item.kind === 'unverified') && hasText(item.text, 120))
}

function errorMessage(status: number) {
  switch (status) {
    case 401: return '学習の期限が切れたか、学習状態を確認できません。新しく学習を始めてください。'
    case 404: return '記録を開けません。閲覧キーと保存期限を確認してください。'
    case 409: return 'このセッションはすでに別の内容で終了しています。'
    case 400: return '入力内容を確認して、もう一度お試しください。'
    case 413: return '入力の上限を超えています。内容を短くしてください。'
    case 429:
    case 503: return 'ただいま混み合っています。少し待ってからお試しください。'
    case 504: return 'ゼロの応答に時間がかかっています。もう一度お試しください。'
    case 502: return invalidResponse
    default: return '混雑または通信エラーでゼロに接続できませんでした。時間をおいてお試しください。'
  }
}

async function post(path: string, body: object, signal: AbortSignal): Promise<unknown> {
  const timeout = deadline(signal, 75_000)
  const base = import.meta.env?.VITE_API_BASE_URL?.trim().replace(/\/+$/, '') || ''
  try {
    let response: Response
    try {
      response = await fetch(`${base}/api/${path}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
        credentials: 'omit',
        cache: 'no-store',
        signal: timeout.signal,
      })
    } catch (error) {
      if (signal.aborted) throw error
      throw new Error(errorMessage(timeout.timedOut ? 504 : 0))
    }
    if (!response.ok) throw new Error(errorMessage(response.status))
    try {
      return await response.json()
    } catch (error) {
      if (signal.aborted) throw error
      throw new Error(timeout.timedOut ? errorMessage(504) : invalidResponse)
    }
  } finally {
    timeout.dispose()
  }
}

export async function startLearning(learningContent: string, signal: AbortSignal) {
  const data = await post('start', { learningContent }, signal)
  if (!isRecord(data) || !isReply(data.reply) || !isSessionId(data.sessionId)) throw new Error(invalidResponse)
  const { sessionId, reply, sessionProof, sessionExpiresAt } = data
  if (sessionProof === undefined && sessionExpiresAt === undefined) return { sessionId, reply }
  if (!isProof(sessionProof) || !isDate(sessionExpiresAt) || Date.parse(sessionExpiresAt) <= Date.now() ||
    Number(sessionProof.split('.')[2]) * 1000 !== Date.parse(sessionExpiresAt)) throw new Error(invalidResponse)
  return { sessionId, reply, sessionProof, sessionExpiresAt }
}

export async function continueLearning(session: LearningRequest, message: string, signal: AbortSignal) {
  const data = await post('chat', { ...session, message }, signal)
  if (!isRecord(data) || !isReply(data.reply)) throw new Error(invalidResponse)
  if (session.sessionProof === undefined && data.sessionProof === undefined) return { reply: data.reply }
  if (!session.sessionProof || !isProof(data.sessionProof) ||
    data.sessionProof.split('.')[2] !== session.sessionProof.split('.')[2]) throw new Error(invalidResponse)
  return { reply: data.reply, sessionProof: data.sessionProof }
}

export async function summarizeLearning(session: LearningRequest, signal: AbortSignal): Promise<LearningResult> {
  const data = await post('summary', session, signal)
  if (!isRecord(data) || data.sessionId !== session.sessionId || !isSummary(data.summary) ||
    data.summary.understood.some(({ evidence }) => !session.history.some(
      (turn) => turn.role === 'user' && turn.content.includes(evidence),
    ))) throw new Error(invalidResponse)
  if (session.sessionProof === undefined && data.saved === undefined && data.viewKey === undefined &&
    data.savedAt === undefined && data.expiresAt === undefined) {
    return { sessionId: session.sessionId, summary: data.summary, saved: false }
  }
  if (!session.sessionProof || data.saved !== true ||
    typeof data.viewKey !== 'string' || !/^[0-9A-HJKMNP-TV-Z]{4}(?:-[0-9A-HJKMNP-TV-Z]{4}){2}$/.test(data.viewKey) ||
    !isDate(data.savedAt) || !isDate(data.expiresAt) || !savedDates(data)) throw new Error(invalidResponse)
  return { sessionId: session.sessionId, summary: data.summary, saved: true, viewKey: data.viewKey, savedAt: data.savedAt, expiresAt: data.expiresAt }
}

export async function openHistory(viewKey: string, signal: AbortSignal): Promise<HistoryRecord> {
  const data = await post('history/open', { viewKey }, signal)
  if (!isRecord(data) || !isSessionId(data.sessionId) || !hasText(data.learningContent, 10000) ||
    !isHistory(data.history, false) || data.history.length < 2 || !isSummary(data.summary) || !savedDates(data)) throw new Error(invalidResponse)
  if (data.summary.understood.some(({ evidence }) => !(data.history as Turn[]).some(turn => turn.role === 'user' && turn.content.includes(evidence)))) throw new Error(invalidResponse)
  return data as HistoryRecord
}

export async function transcribeSpeech(audio: Blob, contentType: string, signal: AbortSignal) {
  const timeout = deadline(signal, 55_000)
  const base = import.meta.env?.VITE_API_BASE_URL?.trim().replace(/\/+$/, '') || ''
  try {
    let response: Response
    try {
      response = await fetch(`${base}/api/speech/transcribe`, {
        method: 'POST',
        headers: { 'Content-Type': contentType },
        body: audio,
        credentials: 'omit',
        cache: 'no-store',
        signal: timeout.signal,
      })
    } catch (error) {
      if (signal.aborted) throw new DOMException('文字起こしを取り消しました。', 'AbortError')
      throw new Error(timeout.timedOut
        ? '文字起こしに時間がかかっています。もう一度お試しください。'
        : '音声文字起こしAPIに接続できませんでした。時間をおいてお試しください。')
    }
    if (!response.ok) {
      const messages: Record<number, string> = {
        401: '音声文字起こしの認証を確認できません。文字で入力してください。',
        404: '音声文字起こしAPIが見つかりません。文字で入力してください。',
        429: '音声文字起こしが混み合っています。少し待つか、文字で入力してください。',
        400: '音声から言葉を確認できませんでした。録り直すか、文字で入力してください。',
        413: '録音が上限を超えています。30秒以内で録音してください。',
        415: 'このブラウザの録音形式には対応していません。文字で入力してください。',
        500: '音声文字起こしの設定を確認してください。',
        502: '文字起こしに失敗しました。もう一度お試しください。',
        503: 'ただいま音声文字起こしを利用できません。時間をおいてお試しください。',
        504: '文字起こしに時間がかかっています。もう一度お試しください。',
      }
      throw new Error(messages[response.status] ?? errorMessage(response.status))
    }
    let data: unknown
    try {
      data = await response.json()
    } catch (error) {
      if (signal.aborted) throw new DOMException('文字起こしを取り消しました。', 'AbortError')
      throw new Error(timeout.timedOut
        ? '文字起こしに時間がかかっています。もう一度お試しください。'
        : invalidResponse)
    }
    if (signal.aborted) throw new DOMException('文字起こしを取り消しました。', 'AbortError')
    if (timeout.timedOut) throw new Error('文字起こしに時間がかかっています。もう一度お試しください。')
    if (!isRecord(data) || !hasText(data.text, ANSWER_LIMIT)) throw new Error(invalidResponse)
    return normalizeText(data.text)
  } finally {
    timeout.dispose()
  }
}

export async function synthesizeSpeech(text: string, signal: AbortSignal): Promise<Blob> {
  const timeout = deadline(signal, 30_000)
  const base = import.meta.env?.VITE_API_BASE_URL?.trim().replace(/\/+$/, '') || ''
  try {
    let response: Response
    try {
      response = await fetch(`${base}/api/speech/synthesize`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text }),
        credentials: 'omit',
        cache: 'no-store',
        signal: timeout.signal,
      })
    } catch (error) {
      if (signal.aborted) throw new DOMException('読み上げを取り消しました。', 'AbortError')
      throw new Error(timeout.timedOut
        ? '読み上げに時間がかかっています。文字で質問を確認してください。'
        : '読み上げAPIに接続できませんでした。文字で質問を確認してください。')
    }
    if (!response.ok) {
      const messages: Record<number, string> = {
        400: '読み上げる質問を確認できませんでした。文字で質問を確認してください。',
        401: '読み上げの認証を確認できません。文字で質問を確認してください。',
        404: '読み上げAPIが見つかりません。文字で質問を確認してください。',
        413: '読み上げる質問が上限を超えています。文字で質問を確認してください。',
        415: '読み上げの送信形式に対応していません。文字で質問を確認してください。',
        429: '読み上げが混み合っています。少し待つか、文字で質問を確認してください。',
        500: '読み上げの設定を確認してください。文字で質問を確認してください。',
        502: '読み上げに失敗しました。文字で質問を確認してください。',
        503: 'ただいま読み上げを利用できません。文字で質問を確認してください。',
        504: '読み上げに時間がかかっています。文字で質問を確認してください。',
      }
      throw new Error(messages[response.status] ?? '読み上げに失敗しました。文字で質問を確認してください。')
    }
    if (response.headers.get('Content-Type')?.split(';')[0].trim().toLowerCase() !== 'audio/mpeg' ||
      Number(response.headers.get('Content-Length') || 0) > 2 * 1024 * 1024) {
      throw new Error('音声を確認できませんでした。文字で質問を確認してください。')
    }
    let audio: Blob
    try {
      audio = await response.blob()
    } catch (error) {
      if (signal.aborted) throw new DOMException('読み上げを取り消しました。', 'AbortError')
      throw new Error(timeout.timedOut
        ? '読み上げに時間がかかっています。文字で質問を確認してください。'
        : '音声を受信できませんでした。文字で質問を確認してください。')
    }
    if (signal.aborted) throw new DOMException('読み上げを取り消しました。', 'AbortError')
    if (timeout.timedOut) throw new Error('読み上げに時間がかかっています。文字で質問を確認してください。')
    if (audio.size === 0 || audio.size > 2 * 1024 * 1024) {
      throw new Error('音声を確認できませんでした。文字で質問を確認してください。')
    }
    return audio
  } finally {
    timeout.dispose()
  }
}
