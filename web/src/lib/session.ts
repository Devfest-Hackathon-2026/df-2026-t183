import { isDate, isHistory, isProof, isSessionId } from './api/learning.ts'
import type { Session } from './api/learning.ts'

export const STORAGE_KEY = 'ai-ni-oshieru.learning.v1'
export type LearningState = {
  version: 1
  session: Session
  expiresAt: string
  draft: string
  checked: boolean[]
  frozen: boolean
}
type StoragePort = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>
export function validState(value: unknown, now = Date.now()): value is LearningState {
  if (!value || typeof value !== 'object') return false
  const s = value as LearningState
  return s.version === 1 && isDate(s.expiresAt) && Date.parse(s.expiresAt) > now && Date.parse(s.expiresAt) <= now + 2 * 60 * 60 * 1000 &&
    typeof s.draft === 'string' && s.draft.length <= 20000 && typeof s.frozen === 'boolean' &&
    Array.isArray(s.checked) && s.checked.length === 3 && s.checked.every(v => typeof v === 'boolean') &&
    !!s.session && isSessionId(s.session.sessionId) && isProof(s.session.sessionProof) &&
    Number(s.session.sessionProof.split('.')[2]) * 1000 === Date.parse(s.expiresAt) &&
    typeof s.session.learningContent === 'string' && s.session.learningContent.trim().length > 0 && Array.from(s.session.learningContent).length <= 10000 &&
    isHistory(s.session.history) && (!s.frozen || s.session.history.length >= 3)
}
export function removeLearning(storage: StoragePort): boolean {
  try { storage.removeItem(STORAGE_KEY); return true } catch { return false }
}
export function loadLearning(storage: StoragePort, now = Date.now()): LearningState | undefined {
  try {
    const raw = storage.getItem(STORAGE_KEY)
    if (!raw) return
    const parsed: unknown = raw.length <= 512 * 1024 ? JSON.parse(raw) : null
    if (validState(parsed, now)) return cleanState(parsed)
  } catch { /* Storage can be unavailable in private browsing. */ }
  removeLearning(storage)
}
function cleanState(state: LearningState): LearningState {
  const { sessionId, sessionProof, learningContent, history } = state.session
  return { version: 1, session: { sessionId, sessionProof, learningContent, history: history.map(({ role, content }) => ({ role, content })) },
    expiresAt: state.expiresAt, draft: state.draft, checked: [...state.checked], frozen: state.frozen }
}
export function saveLearning(storage: StoragePort, state: LearningState): boolean {
  try { storage.setItem(STORAGE_KEY, JSON.stringify(cleanState(state))); return true } catch { return false }
}

export const DRAFT_KEY = 'ai-ni-oshieru.material.v1'
export function loadMaterial(storage: StoragePort, now = Date.now()): { text: string; expiresAt: number } | undefined {
  try {
    const raw = storage.getItem(DRAFT_KEY)
    if (!raw) return
    const draft = JSON.parse(raw)
    if (draft.version === 1 && typeof draft.text === 'string' && draft.text.length <= 100000 &&
      Number.isFinite(draft.expiresAt) && draft.expiresAt > now && draft.expiresAt <= now + 7200000) return { text: draft.text, expiresAt: draft.expiresAt }
    storage.removeItem(DRAFT_KEY)
  } catch { try { storage.removeItem(DRAFT_KEY) } catch { /* Unavailable storage. */ } }
}
export function saveMaterial(storage: StoragePort, text: string, expiresAt: number): boolean {
  try { storage.setItem(DRAFT_KEY, JSON.stringify({ version: 1, text, expiresAt })); return true } catch { return false }
}
export function removeMaterial(storage: StoragePort): boolean {
  try { storage.removeItem(DRAFT_KEY); return true } catch { return false }
}
