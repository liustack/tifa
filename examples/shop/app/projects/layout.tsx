import { useState } from 'react'
import { Outlet } from '@tanstack/react-router'
import { route, type Data } from './+route'
import { Icon } from '../icons'

export const head = ({ loaderData }: { loaderData?: Data }) => ({
  meta: loaderData ? [{ title: `${loaderData.name} · 山海工作台` }] : [],
})

export default function ProjectLayout() {
  const project = route.useLoaderData()
  const [noteOpen, setNoteOpen] = useState(false)
  return (
    <>
      <div className="project-heading">
        <div>
          <p className="eyebrow">YOUR LITTLE BUSINESS</p>
          <h1>{project.name}</h1>
          <p className="project-description">{project.description}</p>
          <p className="project-pending">
            {project.pending > 0 ? `${project.pending} 单待发货` : '订单都已发出'}
          </p>
        </div>
        <button
          className={`quiet-button ${noteOpen ? 'active' : ''}`}
          aria-expanded={noteOpen}
          aria-controls="project-note"
          onClick={() => setNoteOpen(!noteOpen)}
        >
          <Icon name="note" size={17} />
          店铺备忘
          <Icon name="chevron" size={15} />
        </button>
      </div>
      {noteOpen && (
        <aside className="project-note" id="project-note">
          <Icon name="note" size={19} />
          <div>
            <strong>发货前的小提醒</strong>
            <p>检查包装，附上一张手写卡片。每一份心意，都值得好好送达。</p>
          </div>
        </aside>
      )}
      <Outlet />
    </>
  )
}
