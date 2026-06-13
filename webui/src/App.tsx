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
  useToast,
  type SessionMetadata,
} from './index';
import './styles/globals.css';

// ── Types ───────────────────────────────────

type TabId = 'start' | 'sessions' | 'projects' | 'skills' | 'mcp' | 'plugins' | 'marketplace';

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
  const [activeTab, setActiveTab] = useState<TabId>('start');
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null);

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
        {activeTab === 'start' && <StartTab onSessionCreated={handleSessionCreated} />}
        {activeTab === 'sessions' && <SessionsTab selectedId={selectedSessionId} onSelect={setSelectedSessionId} />}
        {activeTab === 'projects' && <ProjectsTab />}
        {/* Other tabs placeholder */}
        {['skills', 'mcp', 'plugins', 'marketplace'].includes(activeTab) && (
          <div style={{ padding: '2rem', textAlign: 'center' }}>
            <EmptyState title={t('portal.coming_soon')} hint={t('portal.coming_soon.hint')} />
          </div>
        )}
      </main>
    </div>
  );
}

interface StartTabProps {
  onSessionCreated: (sessionId: string) => void;
}

function StartTab({ onSessionCreated }: StartTabProps) {
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
      const result = await apiPost<{ session_id: string }>('/api/sessions/start', { prompt });
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
              <Button variant="outline">Settings</Button>
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
}

function SessionsTab({ selectedId: externalSelectedId, onSelect: externalOnSelect }: SessionsTabProps) {
  const [internalSelectedId, setInternalSelectedId] = useState<string | null>(null);
  const { data: sessions, loading } = useSessions();

  // Use external props if provided, otherwise use internal state
  const selectedId = externalSelectedId !== undefined ? externalSelectedId : internalSelectedId;
  const setSelectedId = externalOnSelect || setInternalSelectedId;

  // Transform API sessions to DataList items
  const sessionItems = sessions?.map((s: SessionMetadata) => {
    const timeAgo = formatTimeAgo(s.start_time);
    return {
      id: s.id,
      title: s.cwd ? s.cwd.split('/').pop() || s.id : s.id,
      subtitle: `${timeAgo} · ${s.status}`,
      badge: <Badge variant={s.status === 'running' ? 'success' : 'default'} dot={s.status === 'running'}>{s.status}</Badge>
    };
  }) ?? [];

  return (
    <SplitPane
      masterWidth="320px"
      master={
        loading ? (
          <div style={{ padding: '1rem', textAlign: 'center', color: 'var(--color-text-dim)' }}>Loading...</div>
        ) : (
          <DataList
            items={sessionItems}
            selectedId={selectedId}
            onSelect={setSelectedId}
            selectionLabel="session"
          />
        )
      }
      detail={
        selectedId ? (
          <SessionDetail session={sessions?.find(s => s.id === selectedId)} />
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

function SessionDetail({ session }: { session?: SessionMetadata }) {
  const [entries, setEntries] = useState<any[]>([]);
  const [showResumeInput, setShowResumeInput] = useState(false);
  const [resumePrompt, setResumePrompt] = useState('');
  const [resuming, setResuming] = useState(false);
  const toast = useToast();
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
          <Button variant="ghost" size="sm">Delete</Button>
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

function ProjectsTab() {
  return (
    <div style={{ padding: '2rem', maxWidth: '1000px', margin: '0 auto' }}>
      <h2 style={{ marginBottom: '1.5rem' }}>Projects</h2>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(300px, 1fr))', gap: '1.5rem' }}>
        <GlassPanel interactive style={{ padding: '1.5rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
            <div>
              <h3 style={{ margin: 0 }}>jenny</h3>
              <code style={{ fontSize: '11px', color: 'var(--color-text-muted)' }}>PLACEHOLDER</code>
            </div>
            <Badge variant="success">Active</Badge>
          </div>
          <div style={{ marginTop: '1.5rem', display: 'flex', gap: '1rem' }}>
            <div style={{ fontSize: '12px' }}>
              <div style={{ color: 'var(--color-text-muted)' }}>Total Cost</div>
              <div style={{ fontWeight: 600 }}>$1.23</div>
            </div>
            <div style={{ fontSize: '12px' }}>
              <div style={{ color: 'var(--color-text-muted)' }}>Sessions</div>
              <div style={{ fontWeight: 600 }}>42</div>
            </div>
          </div>
        </GlassPanel>
      </div>
    </div>
  );
}

export default App;