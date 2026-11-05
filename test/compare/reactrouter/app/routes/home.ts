import { redirect } from 'react-router'

// GET / goes to the first shop, as app/route.go of the shop does.
export const loader = () => redirect('/projects/studio')
