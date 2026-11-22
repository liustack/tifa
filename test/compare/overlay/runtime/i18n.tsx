// The module "tifa/i18n" for the comparison apps. The shop's setup.ts imports "tifa/i18n" and adds
// formatters to the instance, so it runs here once the instance exists, as tifa runs it before the
// first route module. The instance is in i18n-core.tsx: setup.ts reads it through this module, which
// re-exports it.
export * from './i18n-core'
import '../setup'
