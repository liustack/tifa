package page

import "time"

// Constants of the app itself. route.go is the app's package, so a name that is near a switch,
// such as Cahce or SSL, is its own and not a typo.
const Cahce = true

const Memoo = false

const SSL = true

const SSG = false

const Demo = true

const Cached = true

const ReadTimout = time.Minute

const BodyLmt = 1 << 20
