import { Link, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import Chip from '@mui/material/Chip'
import FormControl from '@mui/material/FormControl'
import IconButton from '@mui/material/IconButton'
import InputAdornment from '@mui/material/InputAdornment'
import InputLabel from '@mui/material/InputLabel'
import MenuItem from '@mui/material/MenuItem'
import Paper from '@mui/material/Paper'
import Select from '@mui/material/Select'
import Stack from '@mui/material/Stack'
import Tab from '@mui/material/Tab'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableContainer from '@mui/material/TableContainer'
import TableHead from '@mui/material/TableHead'
import TablePagination from '@mui/material/TablePagination'
import TableRow from '@mui/material/TableRow'
import TableSortLabel from '@mui/material/TableSortLabel'
import Tabs from '@mui/material/Tabs'
import TextField from '@mui/material/TextField'
import Toolbar from '@mui/material/Toolbar'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import SearchIcon from '@mui/icons-material/Search'
import FileDownloadIcon from '@mui/icons-material/FileDownload'
import MoreVertIcon from '@mui/icons-material/MoreVert'
import LocalShippingIcon from '@mui/icons-material/LocalShipping'
import { route } from './+route'
import { day, money, statusColor } from '../theme'

export const head = () => ({ meta: [{ title: 'Orders · Northwind admin' }] })

const tabs = ['', 'pending', 'paid', 'shipped', 'refunded']

export default function Orders() {
  const page = route.useLoaderData()
  const search = route.useSearch() as { status?: string }
  const navigate = useNavigate()
  const [selected, setSelected] = useState<string[]>([])
  const [query, setQuery] = useState('')
  const [channel, setChannel] = useState('all')
  const rows = (page.orders ?? []).filter(
    (o) => (channel === 'all' || o.channel === channel) && (query === '' || o.customer.toLowerCase().includes(query.toLowerCase())),
  )
  const toggle = (id: string) => setSelected((s) => (s.includes(id) ? s.filter((x) => x !== id) : [...s, id]))
  return (
    <Stack spacing={2}>
      <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between' }}>
        <Typography variant="h5">Orders</Typography>
        <Button variant="outlined" startIcon={<FileDownloadIcon />}>
          Export
        </Button>
      </Stack>
      <Paper variant="outlined">
        <Tabs value={search.status ?? ''} onChange={(_, v) => navigate({ to: '/orders', search: v ? { status: v } : {} })} sx={{ px: 2, borderBottom: 1, borderColor: 'divider' }}>
          {tabs.map((t) => (
            <Tab key={t} value={t} label={t === '' ? 'All' : t[0].toUpperCase() + t.slice(1)} />
          ))}
        </Tabs>
        <Toolbar sx={{ gap: 2, py: 2 }}>
          <TextField
            size="small"
            placeholder="Search customer"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            slotProps={{ input: { startAdornment: <InputAdornment position="start"><SearchIcon fontSize="small" /></InputAdornment> } }}
          />
          <FormControl size="small" sx={{ minWidth: 160 }}>
            <InputLabel id="channel-label">Channel</InputLabel>
            <Select labelId="channel-label" label="Channel" value={channel} onChange={(e) => setChannel(e.target.value)}>
              <MenuItem value="all">All channels</MenuItem>
              {['Web', 'Mobile', 'Marketplace', 'Retail'].map((c) => (
                <MenuItem key={c} value={c}>
                  {c}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
          <Box sx={{ flexGrow: 1 }} />
          {selected.length > 0 && (
            <Button startIcon={<LocalShippingIcon />} variant="contained">
              Ship {selected.length}
            </Button>
          )}
        </Toolbar>
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell padding="checkbox">
                  <Checkbox
                    indeterminate={selected.length > 0 && selected.length < rows.length}
                    checked={rows.length > 0 && selected.length === rows.length}
                    onChange={(e) => setSelected(e.target.checked ? rows.map((r) => r.id) : [])}
                    slotProps={{ input: { 'aria-label': 'select all orders' } }}
                  />
                </TableCell>
                <TableCell>
                  <TableSortLabel active direction="desc">
                    Order
                  </TableSortLabel>
                </TableCell>
                <TableCell>Customer</TableCell>
                <TableCell>City</TableCell>
                <TableCell>Channel</TableCell>
                <TableCell>Placed</TableCell>
                <TableCell>Status</TableCell>
                <TableCell align="right">Items</TableCell>
                <TableCell align="right">Total</TableCell>
                <TableCell />
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.map((o) => (
                <TableRow key={o.id} hover selected={selected.includes(o.id)}>
                  <TableCell padding="checkbox">
                    <Checkbox checked={selected.includes(o.id)} onChange={() => toggle(o.id)} slotProps={{ input: { 'aria-label': `select ${o.id}` } }} />
                  </TableCell>
                  <TableCell>
                    <Link to="/orders/$order" params={{ order: o.id }}>
                      {o.id}
                    </Link>
                  </TableCell>
                  <TableCell>
                    <Typography variant="body2">{o.customer}</Typography>
                    <Typography variant="caption" color="text.secondary">
                      {o.email}
                    </Typography>
                  </TableCell>
                  <TableCell>{o.city}</TableCell>
                  <TableCell>{o.channel}</TableCell>
                  <TableCell>{day(o.placedAt)}</TableCell>
                  <TableCell>
                    <Chip size="small" label={o.status} color={statusColor[o.status as keyof typeof statusColor]} />
                  </TableCell>
                  <TableCell align="right">{(o.items ?? []).reduce((n, i) => n + i.quantity, 0)}</TableCell>
                  <TableCell align="right">{money(o.totalCents)}</TableCell>
                  <TableCell padding="checkbox">
                    <Tooltip title="More">
                      <IconButton size="small" aria-label={`more for ${o.id}`}>
                        <MoreVertIcon fontSize="small" />
                      </IconButton>
                    </Tooltip>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
        <TablePagination
          component="div"
          count={page.total}
          page={page.page}
          rowsPerPage={page.size}
          rowsPerPageOptions={[page.size]}
          onPageChange={(_, p) => navigate({ to: '/orders', search: { ...search, page: p } })}
        />
      </Paper>
    </Stack>
  )
}
