import { Route, Routes } from 'react-router-dom'
import { TabBar } from './components/Layout'
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

export default function App() {
  return (
    <div className="app">
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
