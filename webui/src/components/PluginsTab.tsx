import { GlassPanel, Badge, EmptyState, LoadingState, SplitPane, DataList, useLocale } from '../index';

// Plugin info type matching the API response
export interface PluginInfo {
  name: string;
  version: string;
  description: string;
  root_path: string;
}

interface PluginsTabProps {
  plugins: PluginInfo[];
  loading: boolean;
  selectedId: string | null;
  onSelect: (id: string | null) => void;
}

export function PluginsTab({ plugins, loading, selectedId, onSelect }: PluginsTabProps) {
  const { t } = useLocale();

  const items = plugins.map((plugin) => ({
    id: plugin.root_path,
    title: plugin.name,
    subtitle: plugin.version || '(no version)',
    badge: plugin.version ? <Badge variant="default">{plugin.version}</Badge> : undefined,
  }));

  const selectedPlugin = plugins.find((p) => p.root_path === selectedId);

  return (
    <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
    <SplitPane
      masterWidth="360px"
      master={
        <div style={{ display: 'flex', flexDirection: 'column', minHeight: 0, height: '100%' }}>
          <div style={{ padding: '1rem 1.25rem', borderBottom: '1px solid var(--color-border)', flexShrink: 0 }}>
            <h2 style={{ margin: 0, fontSize: '0.9375rem', fontWeight: 800, letterSpacing: '-0.02em' }}>
              {t('portal.plugins')}
            </h2>
          </div>
          <div style={{ flex: 1, overflowY: 'auto', minHeight: 0 }}>
            {loading ? (
              <div style={{ padding: '1.5rem', textAlign: 'center' }}>
                <LoadingState label="Loading plugins…" variant="inline" />
              </div>
            ) : plugins.length === 0 ? (
              <div style={{ padding: '1.5rem', textAlign: 'center' }}>
                <p style={{ color: 'var(--color-text-dim)', fontSize: '0.875rem' }}>No plugins installed</p>
              </div>
            ) : (
              <DataList items={items} selectedId={selectedId} onSelect={onSelect} selectionLabel="plugin" />
            )}
          </div>
        </div>
      }
      detail={
        selectedPlugin ? (
          <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
            <header style={{ padding: '1.25rem 1.5rem', borderBottom: '1px solid var(--color-border)', flexShrink: 0 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem' }}>
                <span style={{ fontSize: '1.25rem' }}>🧩</span>
                <h2 style={{ margin: 0, fontSize: '1rem', fontWeight: 700, letterSpacing: '-0.01em' }}>{selectedPlugin.name}</h2>
                {selectedPlugin.version && <Badge variant="default">{selectedPlugin.version}</Badge>}
                <Badge variant="success">Installed</Badge>
              </div>
              <code style={{ fontSize: '11px', color: 'var(--color-text-dim)', fontFamily: 'var(--font-mono)' }}>
                {selectedPlugin.root_path}
              </code>
            </header>
            <div style={{ flex: 1, overflow: 'auto', padding: '1.5rem', display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
              <div>
                <p className="section-label" style={{ marginBottom: '0.5rem' }}>Description</p>
                <p style={{ color: 'var(--color-text)', fontSize: '0.9375rem', lineHeight: 1.6 }}>
                  {selectedPlugin.description || '(no description)'}
                </p>
              </div>
              <div>
                <p className="section-label" style={{ marginBottom: '0.5rem' }}>Root Path</p>
                <GlassPanel style={{ padding: '0.75rem 1rem' }}>
                  <code style={{ fontSize: '0.8125rem', color: 'var(--color-text)', fontFamily: 'var(--font-mono)' }}>
                    {selectedPlugin.root_path}
                  </code>
                </GlassPanel>
              </div>
            </div>
          </div>
        ) : (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--color-text-dim)', fontSize: '0.875rem' }}>
            Select a plugin to view details
          </div>
        )
      }
    />
    </div>
  );
}