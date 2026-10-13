import { readFileSync } from 'node:fs'

globalThis.result = readFileSync('/tmp/secret')
