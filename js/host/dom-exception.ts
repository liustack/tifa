const defineProperty = Object.defineProperty

// The legacy names and codes are part of DOMException, even without a DOM.
const names = ['IndexSize', 'DOMStringSize', 'HierarchyRequest', 'WrongDocument',
  'InvalidCharacter', 'NoDataAllowed', 'NoModificationAllowed', 'NotFound',
  'NotSupported', 'InUseAttribute', 'InvalidState', 'Syntax', 'InvalidModification',
  'Namespace', 'InvalidAccess', 'Validation', 'TypeMismatch', 'Security', 'Network',
  'Abort', 'URLMismatch', 'QuotaExceeded', 'Timeout', 'InvalidNodeType', 'DataClone']

export class DOMException extends Error {
  constructor(message = '', name = 'Error') {
    // Template conversion uses ToString, which rejects symbols, unlike String().
    super(`${message}`)
    defineProperty(this, 'name', { value: `${name}`, configurable: true })
  }
  get code() {
    for (let i = 0; i < names.length; i++) if (names[i] + 'Error' === this.name) return i + 1
    return 0
  }
  get [Symbol.toStringTag]() { return 'DOMException' }
}
for (let i = 0; i < names.length; i++) {
  const key = names[i] === 'URLMismatch' ? 'URL_MISMATCH_ERR'
    : names[i].replace(/([a-z])([A-Z])/g, '$1_$2').toUpperCase() + '_ERR'
  for (const target of [DOMException, DOMException.prototype]) {
    defineProperty(target, key, { value: i + 1, enumerable: true })
  }
}
