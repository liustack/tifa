import { cookies } from 'next/headers'
export const loader = () => cookies()
