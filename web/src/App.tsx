import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { SnapshotContext, useSnapshotProvider } from './hooks/useSnapshot';
import Sidebar from './components/Sidebar';
import HomePage from './pages/HomePage';
import ChatPage from './pages/ChatPage';
import ChannelsPage from './pages/ChannelsPage';
import EmailPage from './pages/EmailPage';
import UsersPage from './pages/UsersPage';
import LoginPage from './pages/LoginPage';
import HealthPage from './pages/HealthPage';
import GatewayPage from './pages/GatewayPage';
import JobsPage from './pages/JobsPage';
import ModelsPage from './pages/ModelsPage';
import PhiPage from './pages/PhiPage';
import SkillsPage from './pages/SkillsPage';
import SystemPage from './pages/SystemPage';
import SchedulePage from './pages/SchedulePage';
import RoutingPage from './pages/RoutingPage';
import SettingsPage from './pages/SettingsPage';
import AddonPage from './pages/AddonPage';

export default function App() {
  const snapshotValue = useSnapshotProvider();

  return (
    <SnapshotContext.Provider value={snapshotValue}>
      <BrowserRouter>
        <div className="flex h-screen bg-surface-300">
          <Sidebar />
          <div className="flex-1 flex flex-col overflow-hidden">
            <Routes>
              <Route path="/" element={<HomePage />} />
              <Route path="/chat" element={<ChatPage />} />
              <Route path="/channels" element={<ChannelsPage />} />
              <Route path="/email" element={<EmailPage />} />
              <Route path="/users" element={<UsersPage />} />
              <Route path="/login" element={<LoginPage />} />
              <Route path="/health" element={<HealthPage />} />
              <Route path="/gateway" element={<GatewayPage />} />
              <Route path="/jobs" element={<JobsPage />} />
              <Route path="/models" element={<ModelsPage />} />
              <Route path="/phi" element={<PhiPage />} />
              <Route path="/skills" element={<SkillsPage />} />
              <Route path="/system" element={<SystemPage />} />
              <Route path="/schedule" element={<SchedulePage />} />
              <Route path="/routing" element={<RoutingPage />} />
              <Route path="/settings" element={<SettingsPage />} />
              <Route path="/addons/:name" element={<AddonPage />} />
            </Routes>
          </div>
        </div>
      </BrowserRouter>
    </SnapshotContext.Provider>
  );
}
