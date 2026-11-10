export const MATERIAL_LIMIT = 10_000
export const ANSWER_LIMIT = 1_000
export const QUESTION_LIMIT = 300
export const TURN_LIMIT = 20
export const FILE_LIMIT_MIB = 5
export const MIB = 1024 ** 2
export const PAGE_LIMIT = 20

export function normalizeText(value: string) {
  return value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, '')
}

export function textLength(value: string) {
  return Array.from(normalizeText(value)).length
}
