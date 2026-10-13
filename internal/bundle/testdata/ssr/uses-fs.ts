import read from 'uses-fs'

globalThis.result = read('/tmp/secret')
