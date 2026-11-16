// performance.now and performance.timeOrigin, the two members that libraries read while
// rendering (framer-motion's motion values read now). Built from Date.now, so a render clock
// that an app sets (tifa.WithRenderClock) moves it too.
// Date.now is taken once, here, so a page that replaces it later does not change them.
const dateNow = Date.now
const timeOrigin = dateNow()
let last = 0

export const performance = {
  timeOrigin,
  now(): number {
    // Never smaller than the last value, so a wall clock set back cannot make it run backwards.
    const value = dateNow() - timeOrigin
    if (value > last) last = value
    return last
  },
}
