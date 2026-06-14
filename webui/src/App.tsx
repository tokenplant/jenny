import React, { useState, useEffect } from 'react';
import {
  AppHeader,
  ToastProvider,
  ConfirmProvider,
  LocaleProvider,
  useLocale,
  useTheme,
  SplitPane,
  DataList,
  Badge,
  SessionEventsPanel,
  StreamPanel,
  StatCard,
  GlassPanel,
  TextField,
  Button,
  EmptyState,
  useStats,
  useSessions,
  useSessionStream,
  killSession,
  apiPost,
  apiGet,
  useApi,
  useToast,
  useConfirm,
  SettingsDialog,
  type SessionMetadata,
} from './index';
import { useSettings, type PortalSettings } from './components/feedback/SettingsDialog';
import { SkillsTab, type SkillInfo } from './components/SkillsTab';
import { MCPServersTab, type MCPServerInfo } from './components/MCPServersTab';
import { PluginsTab, type PluginInfo } from './components/PluginsTab';
import './styles/globals.css';

// ── Types ───────────────────────────────────

type TabId = 'start' | 'sessions' | 'projects' | 'skills' | 'mcp' | 'plugins' | 'marketplace';

// Project group type for grouping sessions by cwd
interface ProjectGroup {
  path: string;        // Full cwd path (empty string for "General")
  name: string;        // Last segment or "General"
  totalSessions: number;
  totalCost: number;
  isActive: boolean;   // Any session running?
  lastActive: number;  // Most recent session start_time
}

// ── Components ──────────────────────────────

function App() {
  return (
    <ToastProvider>
      <ConfirmProvider>
        <LocaleProvider>
          <AppContent />
        </LocaleProvider>
      </ConfirmProvider>
    </ToastProvider>
  );
}

function AppContent() {
  const { theme, setTheme } = useTheme();
  const { t, locale, setLocale } = useLocale();
  const { settings, saveSettings } = useSettings();
  const [activeTab, setActiveTab] = useState<TabId>('start');
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null);
  const [projectFilter, setProjectFilter] = useState<string>('');
  const [showSettings, setShowSettings] = useState(false);
  const [marketplaceBrowseView, setMarketplaceBrowseView] = useState(false);

  // Fetch skills data for the Skills tab
  const { data: skills, loading: skillsLoading } = useApi<SkillInfo[]>('/api/skills');

  // Fetch MCP servers data for the MCP tab
  const { data: mcpServers, loading: mcpServersLoading } = useApi<MCPServerInfo[]>('/api/mcp/servers');

  // Fetch plugins data for the Plugins tab
  const { data: plugins, loading: pluginsLoading } = useApi<PluginInfo[]>('/api/plugins');

  // Callback to handle session creation and navigate to sessions tab
  const handleSessionCreated = (sessionId: string) => {
    setSelectedSessionId(sessionId);
    setActiveTab('sessions');
  };

  return (
    <div style={{ minHeight: '100vh', display: 'flex', flexDirection: 'column' }}>
      <AppHeader
        brand="Jenny Portal"
        tabs={[
          { id: 'start', label: t('portal.start') },
          { id: 'sessions', label: t('portal.sessions') },
          { id: 'projects', label: t('portal.projects') },
          { id: 'skills', label: t('portal.skills') },
          { id: 'mcp', label: t('portal.mcp') },
          { id: 'plugins', label: t('portal.plugins') },
          { id: 'marketplace', label: t('portal.marketplace') },
        ]}
        activeTab={activeTab}
        onTabChange={(id) => {
          setActiveTab(id as TabId);
          setMarketplaceBrowseView(false);
        }}
        theme={theme}
        onThemeChange={setTheme}
        locale={locale}
        onLocaleChange={setLocale}
      />

      <main style={{ flex: 1, position: 'relative', overflow: 'hidden' }}>
        {activeTab === 'start' && <StartTab onSessionCreated={handleSessionCreated} onOpenSettings={() => setShowSettings(true)} settings={settings} />}
        {activeTab === 'sessions' && <SessionsTab selectedId={selectedSessionId} onSelect={setSelectedSessionId} projectFilter={projectFilter} onFilterChange={setProjectFilter} />}
        {activeTab === 'projects' && <ProjectsTab onNavigate={(tab) => setActiveTab(tab as TabId)} onFilter={(cwd) => { setProjectFilter(cwd); setActiveTab('sessions'); }} />}
        {activeTab === 'skills' && <SkillsTab skills={skills ?? []} loading={skillsLoading} />}
        {activeTab === 'mcp' && <MCPServersTab servers={mcpServers ?? []} loading={mcpServersLoading} />}
        {activeTab === 'plugins' && <PluginsTab plugins={plugins ?? []} loading={pluginsLoading} />}
        {activeTab === 'marketplace' && (
          marketplaceBrowseView ? (
            <MarketplaceBrowseView onBack={() => setMarketplaceBrowseView(false)} />
          ) : (
            <MarketplaceTab onNavigate={(tab) => setActiveTab(tab as TabId)} onBrowse={() => setMarketplaceBrowseView(true)} />
          )
        )}
      </main>

      <SettingsDialog open={showSettings} onClose={() => setShowSettings(false)} settings={settings} onSave={saveSettings} />
    </div>
  );
}
interface StartTabProps {
  onSessionCreated: (sessionId: string) => void;
  onOpenSettings: () => void;
  settings: PortalSettings;
}

function StartTab({ onSessionCreated, onOpenSettings, settings }: StartTabProps) {
  const { t } = useLocale();
  const [prompt, setPrompt] = useState('');
  const [launching, setLaunching] = useState(false);
  const toast = useToast();
  const { data: stats, loading } = useStats();

  const formatCost = (cost: number) => {
    if (cost < 0.01) return '$0.00';
    return `$${cost.toFixed(2)}`;
  };

  const handleLaunch = async () => {
    if (!prompt.trim() || launching) return;
    setLaunching(true);
    try {
      // Merge settings into API body (AC4)
      const body: Record<string, string> = {
        prompt: settings.promptPrefix ? `${settings.promptPrefix}\n${prompt}` : prompt,
      };
      if (settings.model) body.model = settings.model;
      if (settings.workingDir) body.cwd = settings.workingDir;
      const result = await apiPost<{ session_id: string }>('/api/sessions/start', body);
      setPrompt('');
      onSessionCreated(result.session_id);
    } catch (err) {
      toast.addToast({ kind: 'error', title: 'Failed to start session', message: err instanceof Error ? err.message : 'Unknown error' });
    } finally {
      setLaunching(false);
    }
  };

  return (
    <div style={{ maxWidth: '800px', margin: '4rem auto', padding: '0 1.5rem', display: 'flex', flexDirection: 'column', gap: '2rem' }}>
      <section style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))', gap: '1rem' }}>
        <StatCard label="Total Sessions" value={loading ? '...' : String(stats?.total_sessions ?? 0)} />
        <StatCard label="Running" value={loading ? '...' : String(stats?.active_sessions ?? 0)} />
        <StatCard label="Total Cost" value={loading ? '...' : formatCost(stats?.total_cost_usd ?? 0)} />
        <StatCard label="Total Tokens" value={loading ? '...' : String(stats?.total_tokens ?? 0)} />
      </section>

      <GlassPanel style={{ padding: '2rem' }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
          <h2 style={{ fontSize: '1.5rem', fontWeight: 600, margin: 0 }}>{t('portal.new_session')}</h2>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
            <TextField
              value={prompt}
              onChange={setPrompt}
              placeholder="What can I help you with today?"
              multiline
              rows={4}
            />
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '1rem' }}>
              <Button variant="outline" onClick={onOpenSettings}>{t('portal.settings')}</Button>
              <Button variant="primary" disabled={!prompt.trim() || launching} onClick={handleLaunch}>
                {launching ? 'Launching...' : t('portal.launch')}
              </Button>
            </div>
          </div>
        </div>
      </GlassPanel>

      <section>
        <h3 className="section-label">{t('portal.recent_projects')}</h3>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '1rem', marginTop: '1rem' }}>
          <GlassPanel interactive style={{ padding: '1rem' }}>
            <div style={{ fontWeight: 600 }}>jenny</div>
            <div style={{ fontSize: '12px', color: 'var(--color-text-muted)' }}>PLACEHOLDER</div>
          </GlassPanel>
          <GlassPanel interactive style={{ padding: '1rem' }}>
            <div style={{ fontWeight: 600 }}>glimpse-ui</div>
            <div style={{ fontSize: '12px', color: 'var(--color-text-muted)' }}>PLACEHOLDER</div>
          </GlassPanel>
        </div>
      </section>
    </div>
  );
}

interface SessionsTabProps {
  selectedId: string | null;
  onSelect: (id: string | null) => void;
  projectFilter: string;
  onFilterChange: (filter: string) => void;
}

function SessionsTab({ selectedId: externalSelectedId, onSelect: externalOnSelect, projectFilter, onFilterChange }: SessionsTabProps) {
  const [internalSelectedId, setInternalSelectedId] = useState<string | null>(null);
  const { data: sessions, loading, refetch } = useSessions();
  const { t } = useLocale();

  // Use external props if provided, otherwise use internal state
  const selectedId = externalSelectedId !== undefined ? externalSelectedId : internalSelectedId;
  const setSelectedId = externalOnSelect || setInternalSelectedId;

  // Filter sessions by projectFilter if set
  const filteredSessions = projectFilter
    ? sessions?.filter((s: SessionMetadata) => s.cwd === projectFilter)
    : sessions;

  // Transform API sessions to DataList items
  const sessionItems = filteredSessions?.map((s: SessionMetadata) => {
    const timeAgo = formatTimeAgo(s.start_time);
    return {
      id: s.id,
      title: s.cwd ? s.cwd.split(/[\\/]/).filter(Boolean).pop() || s.id : s.id,
      subtitle: `${timeAgo} · ${s.status}`,
      badge: <Badge variant={s.status === 'running' ? 'success' : 'default'} dot={s.status === 'running'}>{s.status}</Badge>
    };
  }) ?? [];

  // Handle session deletion - deselect and refresh list
  const handleSessionDeleted = () => {
    setSelectedId(null);
    refetch();
  };

  // Clear filter and show all sessions
  const handleShowAll = () => {
    onFilterChange('');
  };

  return (
    <SplitPane
      masterWidth="320px"
      master={
        loading ? (
          <div style={{ padding: '1rem', textAlign: 'center', color: 'var(--color-text-dim)' }}>Loading...</div>
        ) : (
          <>
            {projectFilter && (
              <div style={{ padding: '0.75rem 1rem', borderBottom: '1px solid var(--color-border)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                <span style={{ fontSize: '12px', color: 'var(--color-text-muted)' }}>Filtered by project</span>
                <Button variant="ghost" size="sm" onClick={handleShowAll}>
                  {t('portal.show_all')}
                </Button>
              </div>
            )}
            <DataList
              items={sessionItems}
              selectedId={selectedId}
              onSelect={setSelectedId}
              selectionLabel="session"
            />
          </>
        )
      }
      detail={
        selectedId ? (
          <SessionDetail session={sessions?.find(s => s.id === selectedId)} onDeleted={handleSessionDeleted} />
        ) : (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--color-text-dim)' }}>
            Select a session to view details
          </div>
        )
      }
    />
  );
}

// Format timestamp to time ago string
function formatTimeAgo(timestamp: number): string {
  const seconds = Math.floor((Date.now() - timestamp) / 1000);
  if (seconds < 60) return 'just now';
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
}

function SessionDetail({ session, onDeleted }: { session?: SessionMetadata; onDeleted?: () => void }) {
  const [entries, setEntries] = useState<any[]>([]);
  const [showResumeInput, setShowResumeInput] = useState(false);
  const [resumePrompt, setResumePrompt] = useState('');
  const [resuming, setResuming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const toast = useToast();
  const { confirm } = useConfirm();
  const isRunning = session?.status === 'running';

  // Connect to SSE stream for real-time updates
  useSessionStream(session?.id ?? '', (data) => {
    setEntries(prev => [...prev, data]);
  });

  const formatCost = (cost?: number) => cost ? `$${cost.toFixed(2)}` : '$0.00';

  const handleResume = async () => {
    if (!session?.id || !resumePrompt.trim() || resuming) return;
    setResuming(true);
    try {
      await apiPost(`/api/sessions/${session.id}/resume`, { prompt: resumePrompt });
      setShowResumeInput(false);
      setResumePrompt('');
      toast.addToast({ kind: 'success', title: 'Session resumed' });
    } catch (err) {
      toast.addToast({ kind: 'error', title: 'Failed to resume session', message: err instanceof Error ? err.message : 'Unknown error' });
    } finally {
      setResuming(false);
    }
  };

  const handleDelete = async () => {
    if (!session?.id || deleting) return;

    const confirmed = await confirm({
      title: 'Delete Session',
      message: 'Are you sure you want to delete this session? This action cannot be undone.',
      confirmLabel: 'Delete',
      cancelLabel: 'Cancel',
      dangerous: true,
    });

    if (!confirmed) return;

    setDeleting(true);
    try {
      await apiPost(`/api/sessions/${session.id}/delete`, {});
      toast.addToast({ kind: 'success', title: 'Session deleted' });
      onDeleted?.();
    } catch (err) {
      toast.addToast({ kind: 'error', title: 'Failed to delete session', message: err instanceof Error ? err.message : 'Unknown error' });
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div style={{ padding: '1.5rem', display: 'flex', flexDirection: 'column', gap: '1.5rem', height: '100%', overflow: 'auto' }}>
      <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem' }}>
            <Badge variant={isRunning ? 'success' : 'default'} dot={isRunning}>{isRunning ? 'Running' : 'Exited'}</Badge>
            <span style={{ fontFamily: 'var(--font-mono)', fontSize: '12px', color: 'var(--color-text-muted)' }}>{session?.id ?? ''}</span>
          </div>
          <h2 style={{ margin: 0, fontSize: '1.25rem' }}>{session?.cwd ?? 'Session'}</h2>
        </div>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          {isRunning ? (
            <Button variant="danger" size="sm" onClick={() => session?.id && killSession(session.id)}>Stop</Button>
          ) : showResumeInput ? (
            <>
              <TextField
                value={resumePrompt}
                onChange={setResumePrompt}
                placeholder="What should I do next?"
                style={{ width: '200px' }}
              />
              <Button variant="primary" size="sm" disabled={!resumePrompt.trim() || resuming} onClick={handleResume}>
                {resuming ? 'Resuming...' : 'Resume'}
              </Button>
              <Button variant="ghost" size="sm" onClick={() => { setShowResumeInput(false); setResumePrompt(''); }}>Cancel</Button>
            </>
          ) : (
            <Button variant="primary" size="sm" onClick={() => setShowResumeInput(true)}>Resume</Button>
          )}
          {/* Delete button is hidden for running sessions */}
          {!isRunning && (
            <Button variant="ghost" size="sm" onClick={handleDelete} disabled={deleting}>
              {deleting ? 'Deleting...' : 'Delete'}
            </Button>
          )}
        </div>
      </header>

      <div className="divider" />

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))', gap: '1rem' }}>
        <StatCard label="Token Usage" value={session?.total_tokens ? String(session.total_tokens) : '0'} />
        <StatCard label="Cost" value={formatCost(session?.total_cost)} />
        <StatCard label="Model" value={session?.model ?? 'unknown'} />
      </div>

      <StreamPanel
        title="Transcript"
        sessionId={session?.id}
        stream="transcript"
        isRunning={isRunning}
        fetchStream={async () => entries.map(e => JSON.stringify(e)).join('\n')}
      />
    </div>
  );
}

// ── Project Groups Hook ───────────────────────────────────────

/**
 * Group sessions by working directory (cwd) and compute aggregated stats.
 * Sessions with empty cwd are grouped under "General".
 */
function useProjectGroups(sessions: SessionMetadata[]): ProjectGroup[] {
  const groups: Record<string, SessionMetadata[]> = {};

  for (const s of sessions) {
    const key = s.cwd || '';
    if (!groups[key]) groups[key] = [];
    groups[key].push(s);
  }

  return Object.entries(groups)
    .map(([path, sessionList]) => ({
      path,
      name: path ? path.split(/[\\/]/).filter(Boolean).pop() || path : 'General',
      totalSessions: sessionList.length,
      totalCost: sessionList.reduce((sum, s) => sum + (s.total_cost || 0), 0),
      isActive: sessionList.some(s => s.status === 'running'),
      lastActive: Math.max(...sessionList.map(s => s.start_time)),
    }))
    .sort((a, b) => b.lastActive - a.lastActive);
}

// ── Projects Tab ─────────────────────────────────────────────

interface ProjectsTabProps {
  onNavigate: (tab: string) => void;
  onFilter: (cwd: string) => void;
}

function ProjectsTab({ onNavigate, onFilter }: ProjectsTabProps) {
  const { data: sessions, loading } = useSessions();
  const { t } = useLocale();

  const groups = useProjectGroups(sessions ?? []);

  if (loading) {
    return (
      <div style={{ padding: '2rem', textAlign: 'center', color: 'var(--color-text-dim)' }}>
        {t('common.loading')}
      </div>
    );
  }

  if (groups.length === 0) {
    return (
      <div style={{ padding: '4rem 2rem', textAlign: 'center', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '1.5rem' }}>
        <EmptyState
          title={t('portal.no_projects')}
          hint={t('portal.no_projects.hint')}
        />
        <Button variant="primary" onClick={() => onNavigate('start')}>
          {t('portal.start')}
        </Button>
      </div>
    );
  }

  return (
    <div style={{ padding: '2rem', maxWidth: '1000px', margin: '0 auto' }}>
      <h2 style={{ marginBottom: '1.5rem' }}>{t('portal.projects')}</h2>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))', gap: '1.5rem' }}>
        {groups.map(group => (
          <GlassPanel
            key={group.path}
            interactive
            style={{ padding: '1.5rem', cursor: 'pointer' }}
            onClick={() => onFilter(group.path)}
          >
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
              <div style={{ overflow: 'hidden' }}>
                <h3 style={{ margin: 0, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{group.name}</h3>
                <code style={{ fontSize: '11px', color: 'var(--color-text-muted)', display: 'block', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                  {group.path || '(no directory)'}
                </code>
              </div>
              <Badge variant={group.isActive ? 'success' : 'default'}>
                {group.isActive ? 'Active' : 'Idle'}
              </Badge>
            </div>
            <div style={{ marginTop: '1.5rem', display: 'flex', gap: '1rem' }}>
              <div style={{ fontSize: '12px' }}>
                <div style={{ color: 'var(--color-text-muted)' }}>Sessions</div>
                <div style={{ fontWeight: 600 }}>{group.totalSessions}</div>
              </div>
              <div style={{ fontSize: '12px' }}>
                <div style={{ color: 'var(--color-text-muted)' }}>Total Cost</div>
                <div style={{ fontWeight: 600 }}>${group.totalCost.toFixed(2)}</div>
              </div>
            </div>
          </GlassPanel>
        ))}
      </div>
    </div>
  );
}

// ── Marketplace Tab ────────────────────────────────────────────

// Marketplace item type from API
interface MarketplaceItem {
  name: string;
  type: 'skill' | 'plugin' | 'mcp';
  description: string;
  version: string;
  download_url: string;
}

interface MarketplaceTabProps {
  onNavigate: (tab: string) => void;
  onBrowse: () => void;
}

function MarketplaceTab({ onNavigate, onBrowse }: MarketplaceTabProps) {
  const { data: skills } = useApi<SkillInfo[]>('/api/skills');
  const { data: mcps } = useApi<MCPServerInfo[]>('/api/mcp/servers');
  const { data: plugins } = useApi<PluginInfo[]>('/api/plugins');
  const { t } = useLocale();

  const categories = [
    {
      id: 'skills',
      name: t('portal.skills'),
      count: skills?.length ?? 0,
      icon: '⚡',
      description: t('marketplace.skills_desc'),
    },
    {
      id: 'mcp',
      name: t('portal.mcp'),
      count: mcps?.length ?? 0,
      icon: '🔌',
      description: t('marketplace.mcp_desc'),
    },
    {
      id: 'plugins',
      name: t('portal.plugins'),
      count: plugins?.length ?? 0,
      icon: '🧩',
      description: t('marketplace.plugins_desc'),
    },
  ];

  const totalInstalled = categories.reduce((sum, c) => sum + c.count, 0);

  if (totalInstalled === 0) {
    return (
      <div style={{ padding: '4rem 2rem', textAlign: 'center' }}>
        <EmptyState
          title={t('marketplace.empty_title')}
          hint={t('marketplace.empty_hint')}
        />
        <div style={{ marginTop: '1.5rem' }}>
          <Button variant="primary" onClick={onBrowse}>Browse Marketplace</Button>
        </div>
      </div>
    );
  }

  return (
    <div style={{ padding: '2rem', maxWidth: '900px', margin: '0 auto' }}>
      <div style={{ marginBottom: '2rem', display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <div>
          <h2 style={{ marginBottom: '0.5rem' }}>{t('portal.marketplace')}</h2>
          <p style={{ color: 'var(--color-text-muted)', margin: 0 }}>
            {t('portal.marketplace_description', { count: totalInstalled })}
          </p>
        </div>
        <Button variant="outline" onClick={onBrowse}>Browse</Button>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))', gap: '1.5rem' }}>
        {categories.map((cat) => (
          <GlassPanel
            key={cat.id}
            interactive
            style={{ padding: '1.5rem', cursor: 'pointer' }}
            onClick={() => onNavigate(cat.id)}
          >
            <div style={{ fontSize: '2rem', marginBottom: '0.75rem' }}>{cat.icon}</div>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <h3 style={{ margin: 0 }}>{cat.name}</h3>
              <Badge variant={cat.count > 0 ? 'success' : 'default'}>
                {t('portal.installed', { count: cat.count })}
              </Badge>
            </div>
            <p
              style={{
                margin: '0.75rem 0 0',
                color: 'var(--color-text-muted)',
                fontSize: '0.875rem',
              }}
            >
              {cat.description}
            </p>
          </GlassPanel>
        ))}
      </div>
    </div>
  );
}

// ── Marketplace Browse View ─────────────────────────────────────

const DEFAULT_MARKETPLACE_URL = 'https://raw.githubusercontent.com/ipy/jenny-marketplace/main/index.json';
const URL_STORAGE_KEY = 'jenny-marketplace-url';

interface MarketplaceBrowseViewProps {
  onBack: () => void;
}

function MarketplaceBrowseView({ onBack }: MarketplaceBrowseViewProps) {
  const { t } = useLocale();
  const toast = useToast();
  const [items, setItems] = React.useState<MarketplaceItem[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const [installed, setInstalled] = React.useState<Set<string>>(new Set());
  const [installing, setInstalling] = React.useState<string | null>(null);
  const [sourceUrl, setSourceUrl] = React.useState<string>(() => {
    try { return localStorage.getItem(URL_STORAGE_KEY) || DEFAULT_MARKETPLACE_URL; }
    catch { return DEFAULT_MARKETPLACE_URL; }
  });

  // Fetch marketplace items using apiGet helper
  React.useEffect(() => {
    const fetchItems = async (url: string) => {
      setLoading(true);
      setError(null);
      try {
        const data = await apiGet<MarketplaceItem[]>(`/api/marketplace/browse?source=${encodeURIComponent(url)}`);
        setItems(data);
        // Persist successful URL
        try { localStorage.setItem(URL_STORAGE_KEY, url); } catch {}
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to fetch marketplace');
      } finally {
        setLoading(false);
      }
    };
    fetchItems(sourceUrl);
  }, []); // Only on mount

  // Get installed names from existing data using apiGet helper
  React.useEffect(() => {
    const updateInstalled = async () => {
      try {
        const skills: SkillInfo[] = await apiGet('/api/skills');
        const names = new Set<string>(skills.map((s) => s.name));
        setInstalled(names);
      } catch {
        // Ignore errors
      }
    };
    updateInstalled();
  }, []);

  const handleBrowse = async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await apiGet<MarketplaceItem[]>(`/api/marketplace/browse?source=${encodeURIComponent(sourceUrl)}`);
      setItems(data);
      // Persist successful URL
      try { localStorage.setItem(URL_STORAGE_KEY, sourceUrl); } catch {}
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch marketplace');
    } finally {
      setLoading(false);
    }
  };

  const handleReset = () => {
    setSourceUrl(DEFAULT_MARKETPLACE_URL);
    handleBrowse();
  };

  const handleInstall = async (item: MarketplaceItem) => {
    if (installed.has(item.name) || installing) return;

    setInstalling(item.name);
    try {
      await apiPost('/api/marketplace/install', {
        type: item.type,
        name: item.name,
        download_url: item.download_url,
      });
      setInstalled((prev) => new Set([...prev, item.name]));
      toast.addToast({ kind: 'success', title: 'Installed', message: `${item.name} has been installed.` });
    } catch (err) {
      const errMsg = err instanceof Error ? err.message : 'Unknown error';
      if (errMsg.includes('409') || errMsg.includes('already installed')) {
        setInstalled((prev) => new Set([...prev, item.name]));
        toast.addToast({ kind: 'info', title: 'Already installed', message: `${item.name} is already installed.` });
      } else {
        toast.addToast({ kind: 'error', title: 'Install failed', message: errMsg });
      }
    } finally {
      setInstalling(null);
    }
  };

  const getTypeIcon = (type: string) => {
    switch (type) {
      case 'skill':
        return '⚡';
      case 'mcp':
        return '🔌';
      case 'plugin':
        return '🧩';
      default:
        return '📦';
    }
  };

  if (loading) {
    return (
      <div style={{ padding: '4rem 2rem', textAlign: 'center' }}>
        <div style={{ color: 'var(--color-text-dim)' }}>{t('common.loading')}</div>
      </div>
    );
  }

  if (error) {
    return (
      <div style={{ padding: '2rem', maxWidth: '900px', margin: '0 auto' }}>
        <div style={{ marginBottom: '1rem', display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <Button variant="ghost" onClick={onBack}>← Back</Button>
          <h2 style={{ margin: 0 }}>{t('portal.marketplace')}</h2>
        </div>

        {/* Marketplace URL input */}
        <div style={{ marginBottom: '1.5rem', display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
          <input
            type="text"
            value={sourceUrl}
            onChange={e => setSourceUrl(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && handleBrowse()}
            style={{
              flex: 1,
              padding: '0.5rem 0.75rem',
              borderRadius: '8px',
              border: '1px solid var(--color-border)',
              background: 'var(--color-surface)',
              color: 'var(--color-text)',
              fontFamily: 'var(--font-mono)',
              fontSize: '0.8125rem',
            }}
            placeholder="https://raw.githubusercontent.com/.../index.json"
          />
          <Button variant="primary" size="sm" onClick={handleBrowse} disabled={loading}>
            Browse
          </Button>
          {sourceUrl !== DEFAULT_MARKETPLACE_URL && (
            <Button variant="ghost" size="sm" onClick={handleReset}>
              Reset
            </Button>
          )}
        </div>

        <EmptyState
          title="Marketplace source unreachable"
          hint={`Failed to fetch from:\n${sourceUrl}\n\nEnter a custom marketplace URL above or ensure the source is available.`}
        />
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <div style={{ padding: '2rem', maxWidth: '900px', margin: '0 auto' }}>
        <div style={{ marginBottom: '1rem', display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <Button variant="ghost" onClick={onBack}>← Back</Button>
          <h2 style={{ margin: 0 }}>{t('portal.marketplace')}</h2>
        </div>

        {/* Marketplace URL input */}
        <div style={{ marginBottom: '1.5rem', display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
          <input
            type="text"
            value={sourceUrl}
            onChange={e => setSourceUrl(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && handleBrowse()}
            style={{
              flex: 1,
              padding: '0.5rem 0.75rem',
              borderRadius: '8px',
              border: '1px solid var(--color-border)',
              background: 'var(--color-surface)',
              color: 'var(--color-text)',
              fontFamily: 'var(--font-mono)',
              fontSize: '0.8125rem',
            }}
            placeholder="https://raw.githubusercontent.com/.../index.json"
          />
          <Button variant="primary" size="sm" onClick={handleBrowse} disabled={loading}>
            Browse
          </Button>
          {sourceUrl !== DEFAULT_MARKETPLACE_URL && (
            <Button variant="ghost" size="sm" onClick={handleReset}>
              Reset
            </Button>
          )}
        </div>

        <EmptyState
          title="No items available"
          hint="No items found in the marketplace. Try a different URL or wait for items to be added."
        />
      </div>
    );
  }

  return (
    <div style={{ padding: '2rem', maxWidth: '900px', margin: '0 auto' }}>
      <div style={{ marginBottom: '1rem', display: 'flex', alignItems: 'center', gap: '1rem' }}>
        <Button variant="ghost" onClick={onBack}>← Back</Button>
        <h2 style={{ margin: 0 }}>{t('portal.marketplace')}</h2>
      </div>

      {/* Marketplace URL input */}
      <div style={{ marginBottom: '1.5rem', display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
        <input
          type="text"
          value={sourceUrl}
          onChange={e => setSourceUrl(e.target.value)}
          onKeyDown={e => e.key === 'Enter' && handleBrowse()}
          style={{
            flex: 1,
            padding: '0.5rem 0.75rem',
            borderRadius: '8px',
            border: '1px solid var(--color-border)',
            background: 'var(--color-surface)',
            color: 'var(--color-text)',
            fontFamily: 'var(--font-mono)',
            fontSize: '0.8125rem',
          }}
          placeholder="https://raw.githubusercontent.com/.../index.json"
        />
        <Button variant="primary" size="sm" onClick={handleBrowse} disabled={loading}>
          Browse
        </Button>
        {sourceUrl !== DEFAULT_MARKETPLACE_URL && (
          <Button variant="ghost" size="sm" onClick={handleReset}>
            Reset
          </Button>
        )}
      </div>

      <div style={{ display: 'grid', gap: '1rem' }}>
        {items.map((item) => {
          const isInstalled = installed.has(item.name);
          const isInstalling = installing === item.name;

          return (
            <GlassPanel key={`${item.type}-${item.name}`} style={{ padding: '1.25rem' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
                <div style={{ flex: 1, overflow: 'hidden' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.25rem' }}>
                    <span style={{ fontSize: '1.25rem' }}>{getTypeIcon(item.type)}</span>
                    <h3 style={{ margin: 0 }}>{item.name}</h3>
                    <Badge variant="default">{item.version}</Badge>
                    <Badge variant="default">{item.type}</Badge>
                  </div>
                  <p
                    style={{
                      margin: '0.5rem 0',
                      color: 'var(--color-text-muted)',
                      fontSize: '0.875rem',
                      overflow: 'hidden',
                      display: '-webkit-box',
                      WebkitLineClamp: 2,
                      WebkitBoxOrient: 'vertical',
                    }}
                  >
                    {item.description || '(no description)'}
                  </p>
                </div>
                <div style={{ marginLeft: '1rem' }}>
                  {isInstalled ? (
                    <Button variant="primary" disabled>
                      Installed
                    </Button>
                  ) : (
                    <Button
                      variant="primary"
                      disabled={isInstalling}
                      onClick={() => handleInstall(item)}
                    >
                      {isInstalling ? 'Installing...' : 'Install'}
                    </Button>
                  )}
                </div>
              </div>
            </GlassPanel>
          );
        })}
      </div>
    </div>
  );
}



export default App;