// Pure helpers for plan 10 (player accounts and game profiles on the phone).

export const LINK_CODE_LENGTH = 6

/** Keeps only the digits of what was typed or pasted (spaces, dashes), at most six. */
export function normaliseCode(input: string): string {
  return input.replace(/\D/g, '').slice(0, LINK_CODE_LENGTH)
}

export const isCompleteCode = (code: string) => new RegExp(`^\\d{${LINK_CODE_LENGTH}}$`).test(code)

/** A game waiting for the player's yes or no. */
export interface ProfileAsk {
  app_id: string
  app_name: string
}

/** Adds an ask to the end of the queue; a second ask for the same game is dropped. */
export function enqueueAsk(queue: readonly ProfileAsk[], ask: ProfileAsk): ProfileAsk[] {
  return queue.some((a) => a.app_id === ask.app_id) ? [...queue] : [...queue, ask]
}

/** The text of the profile sheet. */
export function askText(ask: ProfileAsk, account: string | null): string {
  const name = ask.app_name.trim() || 'A game'
  return `${name} wants to save your progress to ${account || 'your Yui account'}.`
}
