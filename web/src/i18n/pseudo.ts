import { mapMessageText } from './format'

const ACCENTED: Record<string, string> = {
  a: 'á',
  b: 'ƀ',
  c: 'ç',
  d: 'ð',
  e: 'é',
  f: 'ƒ',
  g: 'ĝ',
  h: 'ĥ',
  i: 'í',
  j: 'ĵ',
  k: 'ķ',
  l: 'ļ',
  m: 'ɱ',
  n: 'ñ',
  o: 'ó',
  p: 'þ',
  q: 'ǫ',
  r: 'ŕ',
  s: 'š',
  t: 'ţ',
  u: 'ú',
  v: 'ṽ',
  w: 'ŵ',
  x: 'ẋ',
  y: 'ý',
  z: 'ž',
  A: 'Á',
  B: 'Ɓ',
  C: 'Ç',
  D: 'Ð',
  E: 'É',
  F: 'Ƒ',
  G: 'Ĝ',
  H: 'Ĥ',
  I: 'Í',
  J: 'Ĵ',
  K: 'Ķ',
  L: 'Ļ',
  M: 'Ṁ',
  N: 'Ñ',
  O: 'Ó',
  P: 'Þ',
  Q: 'Ǫ',
  R: 'Ŕ',
  S: 'Š',
  T: 'Ţ',
  U: 'Ú',
  V: 'Ṽ',
  W: 'Ŵ',
  X: 'Ẋ',
  Y: 'Ý',
  Z: 'Ž',
}

/** How much longer the pseudo-locale writes a text: German runs about 30% longer than English. */
export const PSEUDO_GROWTH = 0.35

function accent(text: string): string {
  return [...text].map((char) => ACCENTED[char] ?? char).join('')
}

/**
 * The en-XA form of an English message: letters accented, placeholders and
 * plural or select structure kept, 35% longer, in brackets, so unextracted
 * text (plain) and clipped text (no closing bracket) stand out.
 */
export function pseudoLocalize(message: string): string {
  let letters = 0
  const accented = mapMessageText(message, (text) => {
    letters += text.length
    return accent(text)
  })
  return `[${accented}${'·'.repeat(Math.ceil(letters * PSEUDO_GROWTH))}]`
}
