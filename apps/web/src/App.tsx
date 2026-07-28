import type { ReactNode } from 'react'
import { Route, Routes, useLocation } from 'react-router-dom'
import { AppHeader, Fab, TabBar } from './components/Layout'
import AppForm from './pages/AppForm'
import AppDetail from './pages/AppDetail'
import Apps from './pages/Apps'
import DeploymentDetail from './pages/DeploymentDetail'
import HostDetail from './pages/HostDetail'
import HostForm from './pages/HostForm'
import Hosts from './pages/Hosts'
import NotFound from './pages/NotFound'
import Overview from './pages/Overview'
import Settings from './pages/Settings'

/** The four tab roots, and the floating add button each one carries. Keyed by
 * path because both the header and the button are rendered outside the routes:
 * that is what keeps them on screen, untouched, as you move between tabs.
 * Pushed screens bring their own header instead — see Page. */
const TAB_FABS: Record<string, ReactNode> = {
  '/': null,
  '/hosts': <Fab to="/hosts/new" label="Add a host" />,
  '/apps': <Fab to="/apps/new" label="Add an app" />,
  '/settings': null,
}

export default function App() {
  const { pathname } = useLocation()
  const onTab = Object.prototype.hasOwnProperty.call(TAB_FABS, pathname)

  return (
    <div className="app">
      {onTab && <AppHeader />}
      <Routes>
        <Route path="/" element={<Overview />} />
        <Route path="/hosts" element={<Hosts />} />
        <Route path="/hosts/new" element={<HostForm />} />
        <Route path="/hosts/:id" element={<HostDetail />} />
        <Route path="/hosts/:id/edit" element={<HostForm />} />
        <Route path="/apps" element={<Apps />} />
        <Route path="/apps/new" element={<AppForm />} />
        <Route path="/apps/:id" element={<AppDetail />} />
        <Route path="/apps/:id/edit" element={<AppForm />} />
        <Route path="/deployments/:id" element={<DeploymentDetail />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
      {onTab && TAB_FABS[pathname]}
      <TabBar />
    </div>
  )
}
