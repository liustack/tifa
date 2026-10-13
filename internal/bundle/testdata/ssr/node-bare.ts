import { readFileSync } from 'fs'

globalThis.result = readFileSync('/tmp/secret')
