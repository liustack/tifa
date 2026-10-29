import { counts } from '../../../../domain/loaders.ts'

// The work the server has done since it started, which the comparison reads to check that every
// page request computes its data and renders again.
export const dynamic = 'force-dynamic'

export function GET() {
  return Response.json(counts)
}
