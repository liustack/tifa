import { Outlet } from '@tanstack/react-router'
import { I18nProvider } from 'tifa/i18n'

export default function Shell() {
  return (
    <I18nProvider>
      <div>
        <nav>i18n</nav>
        <Outlet />
      </div>
    </I18nProvider>
  )
}
