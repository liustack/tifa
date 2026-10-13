import Widget from './widget.client'

try {
  Widget()
  globalThis.result = 'called'
} catch (error) {
  globalThis.result = (error as Error).message
}
