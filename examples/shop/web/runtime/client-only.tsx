import { useEffect, useState, type ComponentType, type ReactNode } from 'react'

export function clientOnly<P extends object>(
  load: () => Promise<{ default: ComponentType<P> }>,
  fallback: ReactNode,
) {
  return function ClientOnly(props: P) {
    const [Component, setComponent] = useState<ComponentType<P> | null>(null)
    const [error, setError] = useState<Error | null>(null)
    useEffect(() => {
      let active = true
      load()
        .then((module) => {
          if (active) setComponent(() => module.default)
        })
        .catch((cause) => {
          if (active) setError(cause)
        })
      return () => {
        active = false
      }
    }, [])
    if (error) return <p role="alert">交互模块加载失败，请刷新页面重试。</p>
    return Component ? <Component {...props} /> : fallback
  }
}
