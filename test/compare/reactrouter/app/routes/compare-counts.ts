import { counts } from '../../../domain/counts.ts'

// The work the server has done since it started, which the comparison reads to check that every
// page request computes its data and renders again.
export const loader = () => Response.json(counts)
