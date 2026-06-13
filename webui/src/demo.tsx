import React, { useState } from 'react';
import {
  Button,
  Badge,
  InfoTip,
  FieldLabel,
  IconButton,
  FormField,
  SelectField,
  TextField,
  PageShell,
  EmptyState,
  LoadingState,
  ErrorBanner,
  Card,
  GlassPanel,
  CollapsibleContentBlock,
  Header,
  AppHeader,
  SplitPane,
  ConfirmProvider,
  useConfirm,
  ToastProvider,
  useToast,
  SessionEventsPanel,
  StreamPanel,
  DataList,
  StatCard,
  LocaleProvider,
  useLocale,
  useTheme,
} from './index';
import type { Theme } from './types';
import './styles/globals.css';

// ── Demo App ────────────────────────────────

function DemoApp() {
  const { theme, setTheme } = useTheme();
  const [activeTab, setActiveTab] = useState('overview');
  const [email, setEmail] = useState('');
  const [name, setName] = useState('');

  return (
    <ToastProvider>
      <ConfirmProvider>
        <LocaleProvider>
          <div style={{ minHeight: '100vh', paddingBottom: '4rem' }}>
            <AppHeader
              brand="Glimpse UI"
              tabs={[
                { id: 'overview', label: 'Overview' },
                { id: 'components', label: 'Components' },
                { id: 'complex', label: 'Complex Patterns' },
              ]}
              activeTab={activeTab}
              onTabChange={setActiveTab}
              theme={theme}
              onThemeChange={setTheme}
              locale="en"
              onLocaleChange={(l) => console.log('Locale:', l)}
            />

            <main style={{ maxWidth: '72rem', margin: '0 auto', padding: '2.5rem 1.5rem 4rem', position: 'relative', zIndex: 10 }}>
              {/* ── Colors ── */}
              <section>
                <h2 className="section-label" style={{ marginBottom: '1rem' }}>Brand Colors</h2>
                <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
                  {[
                    { name: 'Primary', color: 'var(--color-primary)' },
                    { name: 'Accent', color: 'var(--color-accent)' },
                    { name: 'Danger', color: 'var(--color-danger)' },
                    { name: 'Success', color: 'var(--color-success)' },
                    { name: 'Warning', color: 'var(--color-warning)' },
                  ].map(({ name, color }) => (
                    <div key={name} style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '0.5rem' }}>
                      <div style={{ width: '60px', height: '40px', background: color, borderRadius: '8px' }} />
                      <span style={{ fontSize: '11px', color: 'var(--color-text-muted)' }}>{name}</span>
                    </div>
                  ))}
                </div>
              </section>

              {/* ── Glass Surfaces ── */}
              <section style={{ marginTop: '2rem' }}>
                <h2 className="section-label" style={{ marginBottom: '1rem' }}>Glass Surfaces</h2>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '1rem' }}>
                  <div className="glass" style={{ padding: '1.5rem', textAlign: 'center' }}>
                    <div style={{ fontWeight: 700, marginBottom: '0.25rem' }}>.glass</div>
                    <div style={{ fontSize: '12px', color: 'var(--color-text-muted)' }}>backdrop-filter blur 40px</div>
                  </div>
                  <div className="glass-subtle" style={{ padding: '1.5rem', textAlign: 'center' }}>
                    <div style={{ fontWeight: 700, marginBottom: '0.25rem' }}>.glass-subtle</div>
                    <div style={{ fontSize: '12px', color: 'var(--color-text-muted)' }}>Lighter glass</div>
                  </div>
                  <div className="glass glow-primary" style={{ padding: '1.5rem', textAlign: 'center' }}>
                    <div style={{ fontWeight: 700, marginBottom: '0.25rem' }}>.glow-primary</div>
                    <div style={{ fontSize: '12px', color: 'var(--color-text-muted)' }}>Selected state</div>
                  </div>
                </div>
              </section>

              {/* ── Buttons ── */}
              <section style={{ marginTop: '2rem' }}>
                <h2 className="section-label" style={{ marginBottom: '1rem' }}>Buttons</h2>
                <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap', alignItems: 'center' }}>
                  <Button variant="default">Default</Button>
                  <Button variant="primary">Primary</Button>
                  <Button variant="accent">Accent</Button>
                  <Button variant="danger">Danger</Button>
                  <Button variant="success">Success</Button>
                  <Button variant="warning">Warning</Button>
                  <Button variant="ghost">Ghost</Button>
                  <Button variant="outline">Outline</Button>
                  <Button variant="text">Text</Button>
                </div>
                <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap', alignItems: 'center', marginTop: '0.5rem' }}>
                  <Button size="xs">XS</Button>
                  <Button size="sm">Small</Button>
                  <Button size="md">Medium</Button>
                  <Button size="lg">Large</Button>
                  <Button loading>Loading</Button>
                  <Button disabled>Disabled</Button>
                  <Button icon="⚙">Icon</Button>
                </div>
              </section>

              {/* ── Badges ── */}
              <section style={{ marginTop: '2rem' }}>
                <h2 className="section-label" style={{ marginBottom: '1rem' }}>Badges</h2>
                <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', alignItems: 'center' }}>
                  <Badge variant="default">Default</Badge>
                  <Badge variant="primary">Primary</Badge>
                  <Badge variant="accent">Accent</Badge>
                  <Badge variant="danger">Danger</Badge>
                  <Badge variant="success" dot>Online</Badge>
                  <Badge variant="warning" dot>Warning</Badge>
                  <Badge variant="primary" size="sm">Small</Badge>
                </div>
              </section>

              {/* ── Form Primitives ── */}
              <section style={{ marginTop: '2rem' }}>
                <h2 className="section-label" style={{ marginBottom: '1rem' }}>Form Primitives</h2>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(280px, 1fr))', gap: '1rem' }}>
                  <GlassPanel>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
                      <FormField label="Email address" tooltip="We never share your email. Hover to see the tooltip.">
                        <TextField type="email" value={email} onChange={setEmail} placeholder="you@example.com" />
                      </FormField>
                      <FormField label="Display name" optional>
                        <TextField value={name} onChange={setName} placeholder="Your name" />
                      </FormField>
                      <FormField label="Language">
                        <SelectField
                          value="en"
                          onChange={() => {}}
                          options={[
                            { value: 'en', label: 'English' },
                            { value: 'zh-Hans', label: '中文 (简体)' },
                            { value: 'ja', label: '日本語' },
                          ]}
                        />
                      </FormField>
                      <div style={{ display: 'flex', gap: '0.5rem' }}>
                        <IconButton label="Settings" variant="default" size="md">⚙</IconButton>
                        <IconButton label="Delete" variant="danger" size="md">🗑</IconButton>
                        <IconButton label="Add" variant="primary" size="sm">+</IconButton>
                        <IconButton label="Close" size="lg">✕</IconButton>
                      </div>
                    </div>
                  </GlassPanel>
                </div>
              </section>

              {/* ── Page States ── */}
              <section style={{ marginTop: '2rem' }}>
                <h2 className="section-label" style={{ marginBottom: '1rem' }}>Page States</h2>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))', gap: '1rem' }}>
                  <EmptyState title="No items yet" hint="Create your first item to get started" icon="📋" />
                  <GlassPanel>
                    <LoadingState label="Loading data…" variant="full" />
                  </GlassPanel>
                  <ErrorBanner message="Failed to load data" onRetry={() => {}} />
                </div>
              </section>

              {/* ── Cards ── */}
              <section style={{ marginTop: '2rem' }}>
                <h2 className="section-label" style={{ marginBottom: '1rem' }}>Cards</h2>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: '1rem' }}>
                  <Card padding glow="primary">
                    <div style={{ fontWeight: 700 }}>Primary Glow</div>
                    <div style={{ fontSize: '12px', color: 'var(--color-text-muted)', marginTop: '0.25rem' }}>Use for selected state</div>
                  </Card>
                  <Card padding glow="accent" interactive>
                    <div style={{ fontWeight: 700 }}>Accent Glow</div>
                    <div style={{ fontSize: '12px', color: 'var(--color-text-muted)', marginTop: '0.25rem' }}>Hover to lift</div>
                  </Card>
                  <Card padding glow="danger">
                    <div style={{ fontWeight: 700 }}>Danger Glow</div>
                    <div style={{ fontSize: '12px', color: 'var(--color-text-muted)', marginTop: '0.25rem' }}>Error state</div>
                  </Card>
                </div>
              </section>

              {/* ── Collapsible ── */}
              <section style={{ marginTop: '2rem' }}>
                <h2 className="section-label" style={{ marginBottom: '1rem' }}>Collapsible</h2>
                <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem', maxWidth: '500px' }}>
                  <CollapsibleContentBlock title="Advanced Settings" variant="default" meta="3 options">
                    <p style={{ fontSize: '14px', color: 'var(--color-text-muted)' }}>Advanced configuration options appear here.</p>
                  </CollapsibleContentBlock>
                  <CollapsibleContentBlock title="Danger Zone" variant="danger">
                    <p style={{ fontSize: '14px', color: 'var(--color-text-muted)' }}>Destructive actions go here.</p>
                  </CollapsibleContentBlock>
                  <CollapsibleContentBlock title="Documentation" variant="muted">
                    <p style={{ fontSize: '14px', color: 'var(--color-text-muted)' }}>Usage documentation and guides.</p>
                  </CollapsibleContentBlock>
                </div>
              </section>

              {/* ── Interactions ── */}
              <section style={{ marginTop: '2rem' }}>
                <h2 className="section-label" style={{ marginBottom: '1rem' }}>Interactions</h2>
                <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
                  <ToastDemo />
                  <ConfirmDemo />
                </div>
              </section>

              {/* ── Complex Patterns ── */}
              <section style={{ marginTop: '2rem' }}>
                <h2 className="section-label" style={{ marginBottom: '1rem' }}>Complex Patterns — SplitPane + DataList</h2>
                <div style={{ height: '400px', display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
                  <SplitPaneComplexDemo />
                </div>
              </section>

              {/* ── Data Display ── */}
              <section style={{ marginTop: '2rem' }}>
                <h2 className="section-label" style={{ marginBottom: '1rem' }}>Data Display</h2>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))', gap: '0.75rem' }}>
                  <StatCard label="Uptime" value="2d 14h" tooltip="Time since server start" />
                  <StatCard label="Running Tasks" value="3" tooltip="Currently active tasks" />
                  <StatCard label="Pending" value="12" />
                  <StatCard label="Active Agents" value="2" tooltip="Connected agent processes" />
                </div>
              </section>

              {/* ── i18n ── */}
              <section style={{ marginTop: '2rem' }}>
                <h2 className="section-label" style={{ marginBottom: '1rem' }}>Internationalization</h2>
                <I18nDemo />
              </section>
            </main>
          </div>
        </LocaleProvider>
      </ConfirmProvider>
    </ToastProvider>
  );
}

// ── Sub-demos ───────────────────────────────

function ToastDemo() {
  const toast = useToast();
  return (
    <GlassPanel style={{ maxWidth: '320px' }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
        <span style={{ fontSize: '12px', fontWeight: 600, color: 'var(--color-text-muted)' }}>Toast</span>
        <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
          <Button size="sm" variant="success" onClick={() => toast.addToast({ kind: 'success', title: 'Saved!', message: 'Your changes were saved.' })}>
            Success
          </Button>
          <Button size="sm" variant="danger" onClick={() => toast.addToast({ kind: 'error', title: 'Failed', message: 'Network error occurred', duration: 0 })}>
            Error (persistent)
          </Button>
          <Button size="sm" variant="default" onClick={() => toast.addToast({ kind: 'info', title: 'Tip', message: 'Keyboard shortcut: ⌘K' })}>
            Info
          </Button>
        </div>
      </div>
    </GlassPanel>
  );
}

function ConfirmDemo() {
  const { confirm } = useConfirm();
  const toast = useToast();
  return (
    <GlassPanel style={{ maxWidth: '320px' }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
        <span style={{ fontSize: '12px', fontWeight: 600, color: 'var(--color-text-muted)' }}>Confirm</span>
        <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
          <Button size="sm" variant="danger" onClick={async () => {
            const ok = await confirm({ title: 'Delete item?', message: 'This action cannot be undone.', dangerous: true, confirmLabel: 'Delete' });
            if (ok) toast.addToast({ kind: 'success', title: 'Deleted!' });
          }}>
            Delete Confirm
          </Button>
          <Button size="sm" variant="default" onClick={async () => {
            const ok = await confirm({ title: 'Leave page?', message: 'Unsaved changes will be lost.' });
            if (ok) toast.addToast({ kind: 'info', title: 'Left the page' });
          }}>
            Leave Confirm
          </Button>
        </div>
      </div>
    </GlassPanel>
  );
}

function SplitPaneComplexDemo() {
  const [selected, setSelected] = useState<string | null>('sess-1');

  const sessions = [
    { id: 'sess-1', title: 'claude', subtitle: 'pid 1234 · running', status: 'running', showKill: true },
    { id: 'sess-2', title: 'claude', subtitle: '5.2s · 14:32:01', status: 'completed', showKill: false },
    { id: 'sess-3', title: 'gemini', subtitle: '2.1s · 14:30:45', status: 'failed', showKill: false },
    { id: 'sess-4', title: 'claude', subtitle: '0.8s · 14:28:12', status: 'completed', showKill: false },
  ];

  const selectedSession = sessions.find((s) => s.id === selected);

  return (
    <SplitPane
      masterWidth="280px"
      master={
        <DataList
          items={sessions.map((s) => ({
            id: s.id,
            title: s.title,
            subtitle: s.subtitle,
            badge: (
              <Badge
                variant={s.status === 'running' ? 'success' : s.status === 'failed' ? 'danger' : 'default'}
                size="sm"
                dot={s.status === 'running'}
              >
                {s.status}
              </Badge>
            ),
            showKill: s.showKill,
            onKill: () => console.log('kill', s.id),
          }))}
          selectedId={selected}
          onSelect={setSelected}
          selectionLabel="session"
          emptyMessage="No sessions"
        />
      }
      detail={
        selectedSession ? (
          <div style={{ padding: '1rem', display: 'flex', flexDirection: 'column', gap: '1rem', flex: 1, overflow: 'auto' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <Badge variant={selectedSession.status === 'running' ? 'success' : 'default'} dot={selectedSession.status === 'running'}>
                {selectedSession.status}
              </Badge>
              <span style={{ fontFamily: 'var(--font-mono)', fontSize: '12px', color: 'var(--color-text-muted)' }}>
                {selectedSession.id}
              </span>
            </div>
            <div className="divider" />
            <SessionEventsPanel
              sessionId={selectedSession.id}
              isRunning={selectedSession.status === 'running'}
              messageCount={8}
              events={[
                { id: '1', kind: 'init', badge: 'INIT', preview: 'Session started', timestamp_ms: Date.now() - 60000 },
                { id: '2', kind: 'user', badge: 'USER', preview: 'Run pipeline: devloop', timestamp_ms: Date.now() - 55000 },
                { id: '3', kind: 'assistant', badge: 'AI', preview: 'Planning 3 subtasks…', timestamp_ms: Date.now() - 50000 },
                { id: '4', kind: 'tool', badge: 'TOOL', preview: 'read_file(path=src/index.ts)', timestamp_ms: Date.now() - 45000, hasResult: true },
                { id: '5', kind: 'result', badge: 'DONE', preview: 'Completed 3/3 subtasks', timestamp_ms: Date.now() - 40000 },
              ]}
            />
            <StreamPanel
              title="stdout"
              sessionId={selectedSession.id}
              stream="stdout"
              isRunning={selectedSession.status === 'running'}
              fetchStream={async () => `[${new Date().toISOString()}] Build started\n[${new Date().toISOString()}] Compiling modules...\n[${new Date().toISOString()}] Done in 1.23s`}
            />
          </div>
        ) : (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', flex: 1, color: 'var(--color-text-dim)' }}>
            Select a session
          </div>
        )
      }
    />
  );
}

function I18nDemo() {
  const { locale, setLocale, t, localeName } = useLocale();
  return (
    <GlassPanel style={{ maxWidth: '400px' }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
          <span style={{ fontSize: '12px', fontWeight: 600, color: 'var(--color-text-muted)' }}>Current:</span>
          <Badge variant="primary">{localeName}</Badge>
        </div>
        <div style={{ display: 'flex', gap: '0.5rem' }}>
          <Button size="sm" variant={locale === 'en' ? 'primary' : 'default'} onClick={() => setLocale('en')}>English</Button>
          <Button size="sm" variant={locale === 'zh-Hans' ? 'primary' : 'default'} onClick={() => setLocale('zh-Hans')}>中文</Button>
        </div>
        <div style={{ borderTop: '1px solid var(--color-border)', paddingTop: '0.75rem' }}>
          <p style={{ fontSize: '13px', color: 'var(--color-text-muted)', marginBottom: '0.5rem' }}>Translations:</p>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem', fontSize: '13px' }}>
            <div><Badge variant="default" size="sm">common.save</Badge> → {t('common.save')}</div>
            <div><Badge variant="default" size="sm">common.cancel</Badge> → {t('common.cancel')}</div>
            <div><Badge variant="default" size="sm">page.empty</Badge> → {t('page.empty')}</div>
            <div><Badge variant="default" size="sm">action.copy</Badge> → {t('action.copy')}</div>
          </div>
        </div>
      </div>
    </GlassPanel>
  );
}

export default DemoApp;