import { useId } from 'react'

import { cn } from '@/lib/utils'

interface LogoProps {
  className?: string
  /** Classes for the word mark, for example to hide it in a collapsed rail. */
  wordmarkClassName?: string
}

/** The pglens mark: a lens over a stacked database glyph. */
export function Logo({ className, wordmarkClassName }: LogoProps) {
  const gradientId = `${useId().replaceAll(':', '')}-logo`
  return (
    <span className={cn('inline-flex items-center gap-2.5', className)}>
      <svg
        aria-hidden="true"
        className="size-8 shrink-0"
        fill="none"
        viewBox="0 0 32 32"
        xmlns="http://www.w3.org/2000/svg"
      >
        <defs>
          <linearGradient id={gradientId} x1="0" x2="32" y1="0" y2="32">
            <stop offset="0" stopColor="#4f9cf7" />
            <stop offset="1" stopColor="#2457c5" />
          </linearGradient>
        </defs>
        <rect fill={`url(#${gradientId})`} height="32" rx="8" width="32" />
        <ellipse cx="14" cy="10.5" fill="#fff" fillOpacity="0.95" rx="6.5" ry="2.5" />
        <path
          d="M7.5 10.5v8c0 1.4 2.9 2.5 6.5 2.5"
          stroke="#fff"
          strokeLinecap="round"
          strokeOpacity="0.95"
          strokeWidth="2"
        />
        <path
          d="M7.5 14.5c0 1.4 2.9 2.5 6.5 2.5"
          stroke="#fff"
          strokeLinecap="round"
          strokeOpacity="0.7"
          strokeWidth="1.6"
        />
        <circle cx="20" cy="19" r="4.25" stroke="#fff" strokeWidth="2.2" />
        <path d="m23.2 22.2 3.3 3.3" stroke="#fff" strokeLinecap="round" strokeWidth="2.4" />
      </svg>
      <span className={cn('text-lg leading-none font-semibold tracking-tight', wordmarkClassName)}>
        pg<span className="font-bold">lens</span>
      </span>
    </span>
  )
}
