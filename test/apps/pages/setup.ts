// Runs once per JavaScript runtime before any route module evaluates. The root Layout reads this
// at its own top level.
;(globalThis as { setupRan?: string }).setupRan = 'ran'
