import { createContext, useContext, useMemo, useState, type ReactNode } from 'react'
import { Direction } from 'radix-ui'

// The providers an admin app stacks around its pages: theme, session and permissions, feature
// flags, formats, toasts, notifications and the command palette. Each holds what its consumers
// read, as in the apps the scenario is sized after.

type Theme = { mode: 'light' | 'dark' | 'system'; density: 'compact' | 'comfortable'; setMode: (m: Theme['mode']) => void }
const ThemeContext = createContext<Theme | null>(null)
export const useTheme = () => need(useContext(ThemeContext), 'ThemeProvider')

type Session = { user: string; login: string; shop: string; role: 'owner' | 'staff' }
const SessionContext = createContext<Session | null>(null)
export const useSession = () => need(useContext(SessionContext), 'SessionProvider')

type Permissions = { can: (action: string) => boolean }
const PermissionsContext = createContext<Permissions | null>(null)
export const usePermissions = () => need(useContext(PermissionsContext), 'PermissionsProvider')

type Flags = Record<string, boolean>
const FlagsContext = createContext<Flags | null>(null)
export const useFlags = () => need(useContext(FlagsContext), 'FlagsProvider')

export type Formats = {
  money: (fen: number) => string
  compactMoney: (fen: number) => string
  count: (n: number) => string
  percent: (share: number) => string
  time: (instant: string) => string
  day: (date: string) => string
  ago: (instant: string) => string
}
const FormatsContext = createContext<Formats | null>(null)
export const useFormats = () => need(useContext(FormatsContext), 'FormatsProvider')

type Toasts = { toasts: string[]; push: (text: string) => void }
const ToastContext = createContext<Toasts | null>(null)
export const useToasts = () => need(useContext(ToastContext), 'ToastProvider')

type Command = { open: boolean; setOpen: (open: boolean) => void }
const CommandContext = createContext<Command | null>(null)
export const useCommand = () => need(useContext(CommandContext), 'CommandProvider')

type Unread = { unread: number; markRead: () => void }
const UnreadContext = createContext<Unread | null>(null)
export const useUnread = () => need(useContext(UnreadContext), 'UnreadProvider')

function need<T>(value: T | null, name: string): T {
  if (value === null) throw new Error(`${name} is missing`)
  return value
}

const formatCache = new Map<string, Formats>()

function formatsFor(timeZone: string, today: string): Formats {
  const key = `${timeZone} ${today}`
  let f = formatCache.get(key)
  if (!f) {
    const money = new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY' })
    const compact = new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY', notation: 'compact', maximumFractionDigits: 1 })
    const count = new Intl.NumberFormat('zh-CN')
    const percent = new Intl.NumberFormat('zh-CN', { style: 'percent', maximumFractionDigits: 1 })
    const time = new Intl.DateTimeFormat('zh-CN', { timeZone, month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
    const day = new Intl.DateTimeFormat('zh-CN', { timeZone: 'UTC', month: 'numeric', day: 'numeric' })
    const relative = new Intl.RelativeTimeFormat('zh-CN', { numeric: 'auto' })
    // Relative times count from the day of the data, not the clock, so the server and the
    // browser write the same text.
    const now = Date.parse(`${today}T09:30:00Z`)
    f = {
      money: (fen) => money.format(fen / 100),
      compactMoney: (fen) => compact.format(fen / 100),
      count: (n) => count.format(n),
      percent: (share) => percent.format(share),
      time: (instant) => time.format(new Date(instant)),
      day: (date) => day.format(new Date(`${date}T00:00:00Z`)),
      ago: (instant) => {
        const minutes = Math.round((Date.parse(instant) - now) / 60000)
        if (Math.abs(minutes) < 60) return relative.format(minutes, 'minute')
        if (Math.abs(minutes) < 1440) return relative.format(Math.round(minutes / 60), 'hour')
        return relative.format(Math.round(minutes / 1440), 'day')
      },
    }
    formatCache.set(key, f)
  }
  return f
}

export function AppProviders({
  session,
  timeZone,
  today,
  unread,
  children,
}: {
  session: Session
  timeZone: string
  today: string
  unread: number
  children: ReactNode
}) {
  const [mode, setMode] = useState<Theme['mode']>('system')
  const theme = useMemo<Theme>(() => ({ mode, density: 'compact', setMode }), [mode])
  const permissions = useMemo<Permissions>(() => ({ can: (action) => session.role === 'owner' || !action.startsWith('refund') }), [session.role])
  const flags = useMemo<Flags>(() => ({ bulkRefund: true, newTimeline: true, commandPalette: true, aiReplies: false }), [])
  const [toasts, setToasts] = useState<string[]>([])
  const toastValue = useMemo<Toasts>(() => ({ toasts, push: (text) => setToasts((t) => [...t, text]) }), [toasts])
  const [open, setOpen] = useState(false)
  const command = useMemo<Command>(() => ({ open, setOpen }), [open])
  const [count, setCount] = useState(unread)
  const unreadValue = useMemo<Unread>(() => ({ unread: count, markRead: () => setCount(0) }), [count])
  return (
    <ThemeContext value={theme}>
      <SessionContext value={session}>
        <PermissionsContext value={permissions}>
          <FlagsContext value={flags}>
            <FormatsContext value={formatsFor(timeZone, today)}>
              <Direction.Provider dir="ltr">
                <ToastContext value={toastValue}>
                  <CommandContext value={command}>
                    <UnreadContext value={unreadValue}>{children}</UnreadContext>
                  </CommandContext>
                </ToastContext>
              </Direction.Provider>
            </FormatsContext>
          </FlagsContext>
        </PermissionsContext>
      </SessionContext>
    </ThemeContext>
  )
}

// Section gives one card of the dashboard a context of its own, the filters it was opened with,
// as dashboards that keep each widget's settings do.
type SectionState = { id: string; range: string }
const SectionContext = createContext<SectionState | null>(null)
export const useSection = () => need(useContext(SectionContext), 'Section')

export function Section({ id, range, children }: { id: string; range: string; children: ReactNode }) {
  const value = useMemo(() => ({ id, range }), [id, range])
  return <SectionContext value={value}>{children}</SectionContext>
}
