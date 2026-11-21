/** A namespace of another language: every key of its English namespace, nothing else. */
export type Translation<T> = { readonly [K in keyof T]: string }
