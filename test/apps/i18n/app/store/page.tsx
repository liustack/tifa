import { useLocation } from '@tanstack/react-router'
import { useLanguage } from 'tifa/client'
import { useTranslation } from 'tifa/i18n'
import { page } from './route.gen'

export default function Store() {
  const data = page.useLoaderData()
  const language = useLanguage()
  const location = useLocation()
  const { t } = useTranslation('store')
  return (
    <main id="store">
      store go={data.language} react={language} path={location.pathname} q={data.q} title={t('title')}
    </main>
  )
}
