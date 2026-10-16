import { useSyncExternalStore } from 'react'

const subscribe = () => () => {}

// useHydrated is false while the page renders on the server and during hydration, and true once
// the browser has taken over. Forms disable their submit button until then, so that pressing
// Enter before the scripts run does not submit the form as a plain HTML form.
export const useHydrated = () =>
  useSyncExternalStore(
    subscribe,
    () => true,
    () => false,
  )
