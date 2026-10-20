import { Link } from '@tanstack/react-router'
import Avatar from '@mui/material/Avatar'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import CardHeader from '@mui/material/CardHeader'
import Chip from '@mui/material/Chip'
import Grid from '@mui/material/Grid'
import LinearProgress from '@mui/material/LinearProgress'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemAvatar from '@mui/material/ListItemAvatar'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import Typography from '@mui/material/Typography'
import TrendingUpIcon from '@mui/icons-material/TrendingUp'
import TrendingDownIcon from '@mui/icons-material/TrendingDown'
import AttachMoneyIcon from '@mui/icons-material/AttachMoney'
import ShoppingCartIcon from '@mui/icons-material/ShoppingCart'
import PeopleIcon from '@mui/icons-material/People'
import UndoIcon from '@mui/icons-material/Undo'
import { route } from './+route'
import { day, money, statusColor } from './theme'

export const head = () => ({ meta: [{ title: 'Dashboard · Northwind admin' }] })

export default function Dashboard() {
  const s = route.useLoaderData()
  const kpis = [
    { label: 'Revenue', value: money(s.revenueCents), delta: 12.4, icon: <AttachMoneyIcon />, color: 'primary.main' },
    { label: 'Orders', value: s.orders.toLocaleString('en-US'), delta: 4.1, icon: <ShoppingCartIcon />, color: 'info.main' },
    { label: 'Customers', value: s.customers.toLocaleString('en-US'), delta: 2.3, icon: <PeopleIcon />, color: 'success.main' },
    { label: 'Refund rate', value: `${(s.refundRate * 100).toFixed(1)}%`, delta: -0.6, icon: <UndoIcon />, color: 'warning.main' },
  ]
  return (
    <Stack spacing={3}>
      <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between' }}>
        <Typography variant="h5">Dashboard</Typography>
        <Button variant="contained" component={Link} to="/orders">
          View orders
        </Button>
      </Stack>
      <Grid container spacing={2}>
        {kpis.map((k) => (
          <Grid key={k.label} size={{ xs: 12, sm: 6, lg: 3 }}>
            <Card>
              <CardContent>
                <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'flex-start' }}>
                  <Box>
                    <Typography variant="overline" color="text.secondary">
                      {k.label}
                    </Typography>
                    <Typography variant="h5">{k.value}</Typography>
                  </Box>
                  <Avatar sx={{ bgcolor: k.color }}>{k.icon}</Avatar>
                </Stack>
                <Stack direction="row" spacing={0.5} sx={{ mt: 1, alignItems: 'center', color: k.delta > 0 ? 'success.main' : 'error.main' }}>
                  {k.delta > 0 ? <TrendingUpIcon fontSize="small" /> : <TrendingDownIcon fontSize="small" />}
                  <Typography variant="body2">{Math.abs(k.delta)}% since last month</Typography>
                </Stack>
              </CardContent>
            </Card>
          </Grid>
        ))}
      </Grid>
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, lg: 8 }}>
          <Card>
            <CardHeader title="Recent orders" action={<Chip label={`${s.byStatus?.pending ?? 0} pending`} color="warning" size="small" />} />
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>Order</TableCell>
                  <TableCell>Customer</TableCell>
                  <TableCell>Placed</TableCell>
                  <TableCell>Status</TableCell>
                  <TableCell align="right">Total</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {(s.recent ?? []).map((o) => (
                  <TableRow key={o.id} hover>
                    <TableCell>
                      <Link to="/orders/$order" params={{ order: o.id }}>
                        {o.id}
                      </Link>
                    </TableCell>
                    <TableCell>{o.customer}</TableCell>
                    <TableCell>{day(o.placedAt)}</TableCell>
                    <TableCell>
                      <Chip size="small" variant="outlined" label={o.status} color={statusColor[o.status as keyof typeof statusColor]} />
                    </TableCell>
                    <TableCell align="right">{money(o.totalCents)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Card>
        </Grid>
        <Grid size={{ xs: 12, lg: 4 }}>
          <Stack spacing={2}>
            <Card>
              <CardHeader title="Sales by channel" />
              <CardContent>
                <Stack spacing={2}>
                  {(s.byChannel ?? []).map((c) => (
                    <Box key={c.name}>
                      <Stack direction="row" sx={{ justifyContent: 'space-between' }}>
                        <Typography variant="body2">{c.name}</Typography>
                        <Typography variant="body2" color="text.secondary">
                          {money(c.value)}
                        </Typography>
                      </Stack>
                      <LinearProgress variant="determinate" value={c.percent * 100} sx={{ height: 8, borderRadius: 4 }} />
                    </Box>
                  ))}
                </Stack>
              </CardContent>
            </Card>
            <Card>
              <CardHeader title="Top products" />
              <List dense>
                {(s.topProducts ?? []).map((p) => (
                  <ListItem key={p.name} secondaryAction={<Typography variant="body2">{p.value} sold</Typography>}>
                    <ListItemAvatar>
                      <Avatar variant="rounded" sx={{ bgcolor: 'grey.200', color: 'text.primary' }}>
                        {p.name.slice(0, 1)}
                      </Avatar>
                    </ListItemAvatar>
                    <ListItemText primary={p.name} secondary={<LinearProgress variant="determinate" color="secondary" value={p.percent * 100} />} />
                  </ListItem>
                ))}
              </List>
            </Card>
          </Stack>
        </Grid>
      </Grid>
    </Stack>
  )
}
