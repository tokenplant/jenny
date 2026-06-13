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
  useApi,
  useToast,
  useConfirm,
  SettingsDialog,
  type SessionMetadata,
} from './index';
import { useSettings, type PortalSettings } from './components/feedback/SettingsDialog';
import { SkillsTab, type SkillInfo } from './components/SkillsTab';
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

  // Fetch skills data for the Skills tab
  const { data: skills, loading: skillsLoading } = useApi<SkillInfo[]>('/api/skills');

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
        onTabChange={(id) => setActiveTab(id as TabId)}
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
        {/* Other tabs placeholder */}
        {['mcp', 'plugins', 'marketplace'].includes(activeTab) && (
          <div style={{ padding: '2rem', textAlign: 'center' }}>
            <EmptyState title={t('portal.coming_soon')} hint={t('portal.coming_soon.hint')} />
          </div>
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
      title: s.cwd ? s.cwd.split('/').filter(Boolean).pop() || s.id : s.id,
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
      name: path ? path.split('/').filter(Boolean).pop() || path : 'General',
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

export default App;