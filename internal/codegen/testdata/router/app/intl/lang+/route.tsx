// A head with links alone, as a page that names its versions in other languages has.
export const head = () => ({ links: [{ rel: 'alternate', hrefLang: 'x-default', href: '/intl' }] })

export default function IntlHome() {
  return <p>INTL_HOME</p>
}
