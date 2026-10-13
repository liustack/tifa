import { useEffect, useState, type ComponentType, type ReactNode } from 'react'

// clientOnly loads a component that needs browser APIs when its module initializes. The SSR
// bundle replaces *.client modules with stubs, so the fallback renders on the server and during
// hydration, and the component replaces it once its module has loaded in the browser.
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
    if (error) throw error
    return Component ? <Component {...props} /> : fallback
  }
}
