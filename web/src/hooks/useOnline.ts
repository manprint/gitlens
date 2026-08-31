import { useEffect, useState } from 'react'

function getInitialOnline(): boolean {
  return typeof navigator === 'undefined' ? true : navigator.onLine
}

export function useOnline(): boolean {
  const [online, setOnline] = useState(getInitialOnline)

  useEffect(() => {
    function handleOnline() {
      setOnline(true)
    }

    function handleOffline() {
      setOnline(false)
    }

    window.addEventListener('online', handleOnline)
    window.addEventListener('offline', handleOffline)
    return () => {
      window.removeEventListener('online', handleOnline)
      window.removeEventListener('offline', handleOffline)
    }
  }, [])

  return online
}
