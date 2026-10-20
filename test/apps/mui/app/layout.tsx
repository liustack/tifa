import { Link, Outlet, useRouterState } from '@tanstack/react-router'
import AppBar from '@mui/material/AppBar'
import Avatar from '@mui/material/Avatar'
import Badge from '@mui/material/Badge'
import Box from '@mui/material/Box'
import CssBaseline from '@mui/material/CssBaseline'
import Divider from '@mui/material/Divider'
import Drawer from '@mui/material/Drawer'
import IconButton from '@mui/material/IconButton'
import InputBase from '@mui/material/InputBase'
import List from '@mui/material/List'
import ListItemButton from '@mui/material/ListItemButton'
import ListItemIcon from '@mui/material/ListItemIcon'
import ListItemText from '@mui/material/ListItemText'
import ListSubheader from '@mui/material/ListSubheader'
import Toolbar from '@mui/material/Toolbar'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import { ThemeProvider, alpha } from '@mui/material/styles'
import DashboardIcon from '@mui/icons-material/Dashboard'
import ReceiptLongIcon from '@mui/icons-material/ReceiptLong'
import PeopleIcon from '@mui/icons-material/People'
import Inventory2Icon from '@mui/icons-material/Inventory2'
import SettingsIcon from '@mui/icons-material/Settings'
import NotificationsIcon from '@mui/icons-material/Notifications'
import SearchIcon from '@mui/icons-material/Search'
import BarChartIcon from '@mui/icons-material/BarChart'
import { route } from './+layout'
import { theme } from './theme'

export const htmlAttributes = { lang: 'en' }

export const documentHead = { meta: [{ title: 'Northwind admin' }] }

const width = 240

export default function Root() {
  const shell = route.useLoaderData()
  const path = useRouterState({ select: (s) => s.location.pathname })
  const nav = [
    { to: '/', label: 'Dashboard', icon: <DashboardIcon /> },
    { to: '/orders', label: 'Orders', icon: <ReceiptLongIcon />, badge: shell.pending },
  ]
  const later = [
    { label: 'Customers', icon: <PeopleIcon /> },
    { label: 'Products', icon: <Inventory2Icon /> },
    { label: 'Reports', icon: <BarChartIcon /> },
    { label: 'Settings', icon: <SettingsIcon /> },
  ]
  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <Box sx={{ display: 'flex' }}>
        <AppBar position="fixed" elevation={0} sx={{ zIndex: (t) => t.zIndex.drawer + 1, borderBottom: 1, borderColor: 'divider' }}>
          <Toolbar>
            <Typography variant="h6" noWrap sx={{ flexGrow: 0, mr: 4 }}>
              Northwind
            </Typography>
            <Box sx={{ position: 'relative', borderRadius: 1, bgcolor: (t) => alpha(t.palette.common.white, 0.15), px: 1, display: 'flex', alignItems: 'center', width: 320 }}>
              <SearchIcon fontSize="small" />
              <InputBase placeholder="Search orders, customers…" sx={{ color: 'inherit', ml: 1, flex: 1 }} inputProps={{ 'aria-label': 'search' }} />
            </Box>
            <Box sx={{ flexGrow: 1 }} />
            <Tooltip title="Notifications">
              <IconButton color="inherit" aria-label="notifications">
                <Badge badgeContent={shell.pending} color="secondary">
                  <NotificationsIcon />
                </Badge>
              </IconButton>
            </Tooltip>
            <Avatar sx={{ ml: 2, bgcolor: 'secondary.main', width: 32, height: 32 }}>{shell.user.slice(0, 1)}</Avatar>
          </Toolbar>
        </AppBar>
        <Drawer variant="permanent" sx={{ width, flexShrink: 0, '& .MuiDrawer-paper': { width, boxSizing: 'border-box' } }}>
          <Toolbar />
          <List subheader={<ListSubheader>Store</ListSubheader>}>
            {nav.map((n) => (
              <ListItemButton key={n.to} component={Link} to={n.to} selected={n.to === '/' ? path === '/' : path.startsWith(n.to)}>
                <ListItemIcon>{n.badge ? <Badge badgeContent={n.badge} color="warning">{n.icon}</Badge> : n.icon}</ListItemIcon>
                <ListItemText primary={n.label} />
              </ListItemButton>
            ))}
          </List>
          <Divider />
          <List subheader={<ListSubheader>Manage</ListSubheader>}>
            {later.map((n) => (
              <ListItemButton key={n.label} disabled>
                <ListItemIcon>{n.icon}</ListItemIcon>
                <ListItemText primary={n.label} />
              </ListItemButton>
            ))}
          </List>
        </Drawer>
        <Box component="main" sx={{ flexGrow: 1, p: 3, minWidth: 0 }}>
          <Toolbar />
          <Outlet />
        </Box>
      </Box>
    </ThemeProvider>
  )
}

export function NotFound() {
  return <Typography sx={{ p: 4 }}>This page does not exist.</Typography>
}
