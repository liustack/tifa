// Page code uses what Vite gives modules: asset imports and import.meta.env. tsc checks that the
// generated tsconfig knows their types.
import logo from './logo.svg'

export const url: string = logo
export const dev: boolean = import.meta.env.DEV
