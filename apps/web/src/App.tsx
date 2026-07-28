import type { ReactNode } from 'react'
import { Link, Route, Routes, useLocation } from 'react-router-dom'
import { AppHeader, TabBar } from './components/Layout'
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

/** The four tab roots, and the action each one carries in the shared header.
 * Keyed by path because the header is rendered outside the routes: that is what
 * keeps it on screen, untouched, as you move between tabs. Pushed screens bring
 * their own header instead — see Page. */
const TAB_ACTIONS: Record<string, ReactNode> = {
  '/': null,
  '/hosts': (
    <Link to="/hosts/new">
      <button className="ghost">Add</button>
    </Link>
  ),
  '/apps': (
    <Link to="/apps/new">
      <button className="ghost">Add</button>
    </Link>
  ),
  '/settings': null,
}

export default function App() {
  const { pathname } = useLocation()
  const onTab = Object.prototype.hasOwnProperty.call(TAB_ACTIONS, pathname)

  return (
    <div className="app">
      {onTab && <AppHeader action={TAB_ACTIONS[pathname]} />}
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
      <TabBar />
    </div>
  )
}
