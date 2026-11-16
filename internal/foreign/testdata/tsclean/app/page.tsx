import { Link, useRouter } from '@tanstack/react-router'
import { clientOnly } from 'tifa/client'
import { ThemeProvider } from 'next-themes'
import { page } from './route.gen'

export const action = 'save'
export const loader = { id: 1 }
export const metadata = 'shop'
export const revalidate = () => 60
export const dynamic = true
export const head = () => ({ meta: [] })
export function Error() { return null }

function getServerSideProps() {}
function createServerFn() {}
createServerFn()

export default function Page() {
  'use strict'
  getServerSideProps()
  return null
}
