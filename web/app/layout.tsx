import { useEffect, useState } from 'react'
import { Link, Outlet, Scripts, useRouterState } from '@tanstack/react-router'
import { useLayoutData } from './layout.generated'
import { useSnapshot } from '@go-quick/data'
import { Icon } from '../ui/icons'

export default function AppLayout() {
  const { shell } = useLayoutData()
  const snapshot = useSnapshot()
  const [collapsed, setCollapsed] = useState(false)
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  })
  const loading = useRouterState({ select: (state) => state.isLoading })
  useEffect(() => {
    document.title = snapshot.title
  }, [snapshot.title])

  return (
    <div className={`app-shell ${collapsed ? 'sidebar-collapsed' : ''}`}>
      <a className="skip-link" href="#main-content">
        跳至主要内容
      </a>
      <aside className="sidebar" aria-label="工作区导航">
        <Link
          className="brand"
          to="/projects/studio"
          aria-label="山海工作台首页"
        >
          <span className="brand-mark">山</span>
          <span className="sidebar-label">
            <strong>山海</strong>
            <small>WORKSPACE</small>
          </span>
        </Link>
        <div className="sidebar-section sidebar-label">我的店铺</div>
        <nav className="project-nav">
          {shell.projects.map((project) => (
            <Link
              key={project.id}
              to={`/projects/${project.id}`}
              className={`nav-link ${
                pathname.startsWith(`/projects/${project.id}`) ? 'selected' : ''
              }`}
              title={project.name}
              aria-current={
                pathname.startsWith(`/projects/${project.id}`)
                  ? 'page'
                  : undefined
              }
            >
              <Icon name={project.id === 'studio' ? 'grid' : 'bag'} />
              <span className="sidebar-label">{project.name}</span>
              <span className="nav-dot" />
            </Link>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <span className="avatar viewer-avatar">{shell.viewer.initials}</span>
          <span className="sidebar-label">
            <strong>{shell.viewer.name}</strong>
            <small>让小生意，慢慢生长。</small>
          </span>
        </div>
      </aside>
      <div className="workspace">
        <header className="topbar">
          <div className="topbar-left">
            <button
              className="icon-button"
              aria-label={collapsed ? '展开侧栏' : '收起侧栏'}
              onClick={() => setCollapsed(!collapsed)}
            >
              <Icon name="panel" />
            </button>
            <span className="topbar-divider" />
            <span>工作台</span>
          </div>
          <span className="demo-badge">
            <span />
            演示工作区
          </span>
        </header>
        {loading && (
          <div
            className="navigation-progress"
            role="status"
            aria-label="正在加载页面"
          />
        )}
        <main id="main-content" className="main-content">
          <Outlet />
        </main>
        <footer className="workspace-footer">
          <span>SHANHAI STUDIO</span>
          <span>这里的订单均为演示数据</span>
        </footer>
      </div>
      <Scripts />
    </div>
  )
}
