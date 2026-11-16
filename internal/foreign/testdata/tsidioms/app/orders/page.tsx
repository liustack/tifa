import { useLoaderData } from '@remix-run/react'
import { redirect } from 'react-router-dom'
import { createFileRoute } from '@tanstack/react-router'
import { createServerFn } from '@tanstack/react-start'
export const loader = async () => ({})
export async function action() {}
export const revalidate = 60
export const dynamic = 'force-dynamic'
export async function getServerSideProps() { return { props: {} } }
export const Route = createFileRoute('/orders')({})
export default function Page() { 'use server'; return null }
