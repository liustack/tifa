import { useState, type ReactNode } from 'react'
import {
  HeadContent,
  Link,
  Outlet,
  Scripts,
  useRouterState,
} from '@tanstack/react-router'
import { route } from './+route'
import { Icon } from './icons'
import './styles.css'

export function Document({ children }: { children: ReactNode }) {
  return (
    <html lang="zh-CN">
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <meta name="theme-color" content="#f7f8f5" />
        <link rel="icon" href="data:," />
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}

export const head = () => ({ meta: [{ title: '山海工作台' }] })

export default function AppLayout() {
  const shell = route.useLoaderData()
  const [collapsed, setCollapsed] = useState(false)
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  })
  const loading = useRouterState({ select: (state) => state.isLoading })

  return (
    <div className={`app-shell ${collapsed ? 'sidebar-collapsed' : ''}`}>
      <a className="skip-link" href="#main-content">
        跳至主要内容
      </a>
      <aside className="sidebar" aria-label="工作区导航">
        <Link
          className="brand"
          to="/projects/$projectId"
          params={{ projectId: 'studio' }}
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
          {(shell.projects ?? []).map((project) => {
            const selected = pathname.startsWith(`/projects/${project.id}`)
            return (
              <Link
                key={project.id}
                to="/projects/$projectId"
                params={{ projectId: project.id }}
                className={`nav-link ${selected ? 'selected' : ''}`}
                title={project.name}
                aria-current={selected ? 'page' : undefined}
              >
                <Icon name={project.id === 'studio' ? 'grid' : 'bag'} />
                <span className="sidebar-label">{project.name}</span>
                <span className="nav-dot" />
              </Link>
            )
          })}
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
    </div>
  )
}

// NotFound shows inside this Layout when a project does not exist, and on its own for a URL
// that matches no page, so it reads no loader data.
export function NotFound() {
  return (
    <section className="not-found" aria-labelledby="not-found-title">
      <Icon name="search" size={28} />
      <h1 id="not-found-title">没有找到这个页面</h1>
      <p>地址可能有误，或者这家店铺已经不在了。</p>
      <a className="quiet-button" href="/projects/studio">
        返回工作台
      </a>
    </section>
  )
}
