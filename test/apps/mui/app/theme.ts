import { createTheme } from '@mui/material/styles'

export const theme = createTheme({
  palette: { primary: { main: '#3f51b5' }, secondary: { main: '#f50057' }, background: { default: '#f4f6f8' } },
  shape: { borderRadius: 10 },
  typography: { fontFamily: 'Roboto, "Helvetica Neue", Arial, sans-serif', h5: { fontWeight: 600 } },
  components: { MuiCard: { defaultProps: { variant: 'outlined' } } },
})

export const money = (cents: number) =>
  new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(cents / 100)

export const day = (iso: string) =>
  new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', timeZone: 'UTC' }).format(new Date(iso))

export const statusColor = { pending: 'warning', paid: 'info', shipped: 'success', refunded: 'default' } as const
