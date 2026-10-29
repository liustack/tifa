// The project domain, ported from examples/shop/app/projects/project.go: the shops of the
// workspace. A project counts its orders waiting to ship through the orders module.
import { NotFound } from './errors.ts'
import { countPending } from './orders.ts'

export interface Project {
  id: string
  name: string
  description: string
  timeZone: string
  pending: number
}

interface ProjectRecord {
  id: string
  name: string
  description: string
  timeZone: string
}

const projects: ProjectRecord[] = [
  { id: 'studio', name: '山海工作室', description: '为日常生活，挑选一些值得留下的物件。', timeZone: 'Asia/Shanghai' },
  { id: 'weekend', name: '周末小店', description: '把轻松带回家。', timeZone: 'Asia/Shanghai' },
]

function withStats(p: ProjectRecord): Project {
  return { id: p.id, name: p.name, description: p.description, timeZone: p.timeZone, pending: countPending(p.id) }
}

// getProject returns the project id, or throws NotFound, as tifa.ErrNotFound.
export function getProject(id: string): Project {
  const p = projects.find((p) => p.id === id)
  if (!p) throw new NotFound(id)
  return withStats(p)
}

export function listProjects(): Project[] {
  return projects.map(withStats)
}
