import { i18n } from 'tifa/i18n'

// The currency and time zone of each language of the storefront, as storekit gives them in Go.
const regions: Record<string, { currency: string; timeZone: string }> = {
  'zh-CN': { currency: 'CNY', timeZone: 'Asia/Shanghai' },
  'en-US': { currency: 'USD', timeZone: 'America/Los_Angeles' },
}

// regionOf fails for a language the storefront does not have, which no message of it can get.
function regionOf(lng: string | undefined) {
  const region = lng === undefined ? undefined : regions[lng]
  if (region === undefined) throw new Error(`the storefront has no currency and time zone for the language ${lng}`)
  return region
}

const dateFields = ['dateStyle', 'timeStyle', 'weekday', 'year', 'month', 'day', 'hour', 'minute', 'second']

// onStart replaces the built-in currency and datetime formats with ones that take the language's
// currency and time zone unless a message names others, so messages write {{amount, currency}} and
// {{date, datetime}} and the server and every browser format them alike. datetime also takes the
// dates Go sends as strings.
export function onStart() {
  i18n.services.formatter?.addCached('currency', (lng, options) => {
    const f = new Intl.NumberFormat(lng, { currency: regionOf(lng).currency, ...options, style: 'currency' })
    return (value) => f.format(value)
  })
  i18n.services.formatter?.addCached('datetime', (lng, options) => {
    // A message that names its own fields, such as month and day, does not get the default
    // dateStyle: Intl refuses the two together.
    const own = dateFields.some((k) => k in options)
    const f = new Intl.DateTimeFormat(lng, { ...(own ? {} : { dateStyle: 'medium' }), timeZone: regionOf(lng).timeZone, ...options })
    return (value) => f.format(typeof value === 'string' ? new Date(value) : value)
  })
}
