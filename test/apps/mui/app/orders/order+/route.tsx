import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Breadcrumbs from '@mui/material/Breadcrumbs'
import Button from '@mui/material/Button'
import Card from '@mui/material/Card'
import CardContent from '@mui/material/CardContent'
import CardHeader from '@mui/material/CardHeader'
import Chip from '@mui/material/Chip'
import Divider from '@mui/material/Divider'
import FormControl from '@mui/material/FormControl'
import FormControlLabel from '@mui/material/FormControlLabel'
import FormHelperText from '@mui/material/FormHelperText'
import FormLabel from '@mui/material/FormLabel'
import Grid from '@mui/material/Grid'
import InputLabel from '@mui/material/InputLabel'
import MenuItem from '@mui/material/MenuItem'
import Radio from '@mui/material/Radio'
import RadioGroup from '@mui/material/RadioGroup'
import Select from '@mui/material/Select'
import Stack from '@mui/material/Stack'
import Step from '@mui/material/Step'
import StepLabel from '@mui/material/StepLabel'
import Stepper from '@mui/material/Stepper'
import Switch from '@mui/material/Switch'
import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableCell from '@mui/material/TableCell'
import TableHead from '@mui/material/TableHead'
import TableRow from '@mui/material/TableRow'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import SaveIcon from '@mui/icons-material/Save'
import PrintIcon from '@mui/icons-material/Print'
import { route, type Data } from './+route'
import { day, money, statusColor } from '../../theme'

export const head = ({ loaderData }: { loaderData?: Data }) => ({ meta: [{ title: `${loaderData?.id ?? 'Order'} · Northwind admin` }] })

const steps = ['Placed', 'Paid', 'Packed', 'Shipped', 'Delivered']
const reached = { pending: 0, paid: 1, shipped: 3, refunded: 1 } as const

export default function OrderPage() {
  const o = route.useLoaderData()
  const [carrier, setCarrier] = useState('ups')
  const [tracking, setTracking] = useState('')
  const [speed, setSpeed] = useState('ground')
  const [notify, setNotify] = useState(true)
  const [gift, setGift] = useState(false)
  const trackingError = tracking !== '' && !/^[A-Z0-9]{10,20}$/.test(tracking)
  const items = o.items ?? []
  return (
    <Stack spacing={2}>
      <Breadcrumbs>
        <Link to="/">Dashboard</Link>
        <Link to="/orders">Orders</Link>
        <Typography color="text.primary">{o.id}</Typography>
      </Breadcrumbs>
      <Stack direction="row" spacing={2} sx={{ alignItems: 'center' }}>
        <Typography variant="h5">{o.id}</Typography>
        <Chip label={o.status} color={statusColor[o.status as keyof typeof statusColor]} />
        <Box sx={{ flexGrow: 1 }} />
        <Button variant="outlined" startIcon={<PrintIcon />}>
          Packing slip
        </Button>
      </Stack>
      <Card>
        <CardContent>
          <Stepper activeStep={reached[o.status as keyof typeof reached]} alternativeLabel>
            {steps.map((s) => (
              <Step key={s}>
                <StepLabel>{s}</StepLabel>
              </Step>
            ))}
          </Stepper>
        </CardContent>
      </Card>
      {o.status === 'refunded' && <Alert severity="info">This order was refunded. Shipping is closed.</Alert>}
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, md: 7 }}>
          <Card>
            <CardHeader title="Items" subheader={`Placed ${day(o.placedAt)} via ${o.channel}`} />
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>SKU</TableCell>
                  <TableCell>Product</TableCell>
                  <TableCell align="right">Qty</TableCell>
                  <TableCell align="right">Price</TableCell>
                  <TableCell align="right">Total</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {items.map((it, i) => (
                  <TableRow key={i}>
                    <TableCell>{it.sku}</TableCell>
                    <TableCell>{it.name}</TableCell>
                    <TableCell align="right">{it.quantity}</TableCell>
                    <TableCell align="right">{money(it.priceCents)}</TableCell>
                    <TableCell align="right">{money(it.priceCents * it.quantity)}</TableCell>
                  </TableRow>
                ))}
                <TableRow>
                  <TableCell colSpan={4} align="right">
                    <strong>Total</strong>
                  </TableCell>
                  <TableCell align="right">
                    <strong>{money(o.totalCents)}</strong>
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </Card>
        </Grid>
        <Grid size={{ xs: 12, md: 5 }}>
          <Card>
            <CardHeader title="Customer" />
            <CardContent>
              <Stack spacing={2}>
                <TextField label="Name" defaultValue={o.customer} fullWidth size="small" />
                <TextField label="Email" defaultValue={o.email} fullWidth size="small" type="email" />
                <TextField label="City" defaultValue={o.city} fullWidth size="small" />
                <TextField label="Delivery notes" defaultValue={o.notes} fullWidth size="small" multiline minRows={2} />
              </Stack>
            </CardContent>
          </Card>
        </Grid>
      </Grid>
      <Card>
        <CardHeader title="Ship this order" />
        <Divider />
        <CardContent>
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 4 }}>
              <FormControl fullWidth size="small">
                <InputLabel id="carrier-label">Carrier</InputLabel>
                <Select labelId="carrier-label" label="Carrier" value={carrier} onChange={(e) => setCarrier(e.target.value)}>
                  <MenuItem value="ups">UPS</MenuItem>
                  <MenuItem value="fedex">FedEx</MenuItem>
                  <MenuItem value="usps">USPS</MenuItem>
                  <MenuItem value="dhl">DHL</MenuItem>
                </Select>
              </FormControl>
            </Grid>
            <Grid size={{ xs: 12, md: 8 }}>
              <TextField
                label="Tracking number"
                value={tracking}
                onChange={(e) => setTracking(e.target.value.toUpperCase())}
                error={trackingError}
                helperText={trackingError ? '10 to 20 letters or digits' : ' '}
                fullWidth
                size="small"
              />
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              <FormControl>
                <FormLabel id="speed-label">Speed</FormLabel>
                <RadioGroup row aria-labelledby="speed-label" value={speed} onChange={(e) => setSpeed(e.target.value)}>
                  <FormControlLabel value="ground" control={<Radio />} label="Ground" />
                  <FormControlLabel value="2day" control={<Radio />} label="2 day" />
                  <FormControlLabel value="overnight" control={<Radio />} label="Overnight" />
                </RadioGroup>
                <FormHelperText>Overnight orders leave before 3 pm.</FormHelperText>
              </FormControl>
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              <Stack>
                <FormControlLabel control={<Switch checked={notify} onChange={(e) => setNotify(e.target.checked)} />} label="Email the customer" />
                <FormControlLabel control={<Switch checked={gift} onChange={(e) => setGift(e.target.checked)} />} label="Gift wrap" />
              </Stack>
            </Grid>
          </Grid>
        </CardContent>
        <Divider />
        <Stack direction="row" spacing={1} sx={{ p: 2, justifyContent: 'flex-end' }}>
          <Button component={Link} to="/orders">
            Cancel
          </Button>
          <Button variant="contained" startIcon={<SaveIcon />} disabled={o.status === 'refunded' || tracking === '' || trackingError}>
            Mark as shipped
          </Button>
        </Stack>
      </Card>
    </Stack>
  )
}

export function NotFound() {
  return <Alert severity="warning">No such order.</Alert>
}
