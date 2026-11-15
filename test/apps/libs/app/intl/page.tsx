import {
  IntlProvider, FormattedMessage, FormattedNumber, FormattedDate, FormattedTime, FormattedDateTimeRange, FormattedRelativeTime, FormattedList, FormattedDisplayName, FormattedPlural,
  useIntl, type MessageDescriptor,
} from 'react-intl'
import type { ReactNode } from 'react'
import { page, type PageData } from './route.gen'

type Stay = PageData['stays'][number]

export const head = () => ({ meta: [{ title: 'Intl · Library pages' }] })

// The messages in their ICU source: IntlProvider parses each one when the page renders, as an app
// that ships uncompiled messages does.
const msgs = {
  title: { id: 'title', defaultMessage: '{count, plural, =0 {No stays} one {# stay} other {# stays}} in {city}' },
  greet: { id: 'greet', defaultMessage: 'Welcome back, <b>{name}</b>! {gender, select, female {She has} male {He has} other {They have}} {points, number} points.' },
  nights: { id: 'nights', defaultMessage: '{n, plural, one {# night} other {# nights}}, {guests, plural, one {# guest} other {# guests}}' },
  rank: { id: 'rank', defaultMessage: 'Your {rank, selectordinal, one {#st} two {#nd} few {#rd} other {#th}} booking this year' },
  price: { id: 'price', defaultMessage: '{total, number, ::currency/EUR} total, {perNight, number, ::currency/EUR} a night' },
  rating: { id: 'rating', defaultMessage: 'Rated {score, number, ::.0} from {reviews, plural, one {# review} other {# reviews}}' },
  left: { id: 'left', defaultMessage: '{rooms, plural, =1 {Only one room left!} few {Only # rooms left} other {# rooms left}}' },
  dist: { id: 'dist', defaultMessage: '{km, number, ::unit/kilometer unit-width-short .0} from the centre' },
  free: { id: 'free', defaultMessage: 'Free cancellation until {date, date, medium} at {date, time, short}' },
  footer: { id: 'footer', defaultMessage: 'Prices include {vat, number, ::percent} VAT. Updated {when}.' },
} satisfies Record<string, MessageDescriptor & { defaultMessage: string }>
type Key = keyof typeof msgs
const LOCALES: Record<string, Partial<Record<Key, string>>> = {
  'en-US': {},
  de: {
    title: '{count, plural, =0 {Keine Unterkünfte} one {# Unterkunft} other {# Unterkünfte}} in {city}',
    greet: 'Willkommen zurück, <b>{name}</b>! {gender, select, female {Sie hat} male {Er hat} other {Sie haben}} {points, number} Punkte.',
    nights: '{n, plural, one {# Nacht} other {# Nächte}}, {guests, plural, one {# Gast} other {# Gäste}}',
    rank: 'Ihre {rank}. Buchung in diesem Jahr',
    price: '{total, number, ::currency/EUR} gesamt, {perNight, number, ::currency/EUR} pro Nacht',
    rating: 'Bewertet mit {score, number, ::.0} aus {reviews, plural, one {# Bewertung} other {# Bewertungen}}',
    left: '{rooms, plural, =1 {Nur noch ein Zimmer!} other {Noch # Zimmer}}',
    dist: '{km, number, ::unit/kilometer unit-width-short .0} vom Zentrum',
    free: 'Kostenlose Stornierung bis {date, date, medium} um {date, time, short}',
    footer: 'Preise inkl. {vat, number, ::percent} MwSt. Aktualisiert {when}.',
  },
  pl: {
    title: '{count, plural, =0 {Brak noclegów} one {# nocleg} few {# noclegi} many {# noclegów} other {# noclegu}} w {city}',
    greet: 'Witaj ponownie, <b>{name}</b>! {gender, select, female {Ma} male {Ma} other {Mają}} {points, number} punktów.',
    nights: '{n, plural, one {# noc} few {# noce} many {# nocy} other {# nocy}}, {guests, plural, one {# gość} few {# gości} many {# gości} other {# gościa}}',
    rank: 'Twoja {rank}. rezerwacja w tym roku',
    price: '{total, number, ::currency/EUR} łącznie, {perNight, number, ::currency/EUR} za noc',
    rating: 'Ocena {score, number, ::.0} z {reviews, plural, one {# opinii} few {# opinii} many {# opinii} other {# opinii}}',
    left: '{rooms, plural, one {Został tylko # pokój!} few {Zostały # pokoje} many {Zostało # pokoi} other {Zostało # pokoju}}',
    dist: '{km, number, ::unit/kilometer unit-width-short .0} od centrum',
    free: 'Bezpłatne anulowanie do {date, date, medium}, {date, time, short}',
    footer: 'Ceny zawierają {vat, number, ::percent} VAT. Zaktualizowano {when}.',
  },
}

function StayCard({ s }: { s: Stay }) {
  const intl = useIntl()
  const am = s.amenities.map((a) => intl.formatMessage({ id: 'am.' + a, defaultMessage: a }))
  return (
    <article className="stay">
      <h3>
        {s.name}{' '}
        <small>
          <FormattedDisplayName type="region" value={s.country} />
        </small>
      </h3>
      <p>
        <FormattedMessage {...msgs.nights} values={{ n: s.nights, guests: s.guests }} /> · <FormattedDateTimeRange from={s.from} to={s.from + s.nights * 86400000} month="short" day="numeric" timeZone="UTC" />
      </p>
      <p>
        <FormattedMessage {...msgs.price} values={{ total: s.perNight * s.nights, perNight: s.perNight }} /> (<FormattedNumber value={s.perNight * s.nights * 1.08} style="currency" currency={s.currency} />)
      </p>
      <p>
        <FormattedMessage {...msgs.rating} values={{ score: s.score, reviews: s.reviews }} /> · <FormattedMessage {...msgs.dist} values={{ km: s.km }} />
      </p>
      <p className="warn">
        <FormattedMessage {...msgs.left} values={{ rooms: s.rooms }} />
      </p>
      <p>
        <FormattedMessage {...msgs.free} values={{ date: s.freeUntil }} />
      </p>
      <p>
        <FormattedList type="conjunction" value={am} /> · <FormattedNumber value={s.reviews} notation="compact" /> · <FormattedNumber value={s.score / 10} style="percent" maximumFractionDigits={1} />
      </p>
      <p>
        <FormattedDate value={s.from} weekday="long" year="numeric" month="long" day="numeric" timeZone="UTC" /> <FormattedTime value={s.freeUntil} timeZone="UTC" /> ·{' '}
        <FormattedRelativeTime value={s.updated} unit="minute" numeric="auto" />
      </p>
    </article>
  )
}

function Results({ stays, now }: { stays: Stay[]; now: number }) {
  const intl = useIntl()
  return (
    <section>
      <h2>
        <FormattedMessage {...msgs.title} values={{ count: stays.length, city: 'Kraków' }} />
      </h2>
      <p>
        <FormattedMessage {...msgs.greet} values={{ name: 'Ada', gender: 'female', points: 12840, b: (c: ReactNode[]) => <b>{c}</b> }} />
      </p>
      <p>
        <FormattedMessage {...msgs.rank} values={{ rank: 3 }} /> · <FormattedPlural value={stays.length} one="stay" other="stays" />
      </p>
      {stays.map((s) => (
        <StayCard key={s.id} s={s} />
      ))}
      <footer>
        {intl.formatMessage(msgs.footer, { vat: 0.08, when: intl.formatRelativeTime(-3, 'hour') })} {intl.formatNumber(1234567.891)} {intl.formatDate(now, { dateStyle: 'full', timeZone: 'UTC' })}
      </footer>
    </section>
  )
}

export default function IntlPage() {
  const { stays, now } = page.useLoaderData()
  return (
    <main>
      {Object.entries(LOCALES).map(([locale, own]) => {
        const messages: Record<string, string> = {}
        for (const k of Object.keys(msgs) as Key[]) messages[k] = own[k] ?? msgs[k].defaultMessage
        return (
          <IntlProvider key={locale} locale={locale} messages={messages} defaultLocale="en-US" timeZone="UTC" onError={() => {}}>
            <Results stays={stays} now={now} />
          </IntlProvider>
        )
      })}
    </main>
  )
}
