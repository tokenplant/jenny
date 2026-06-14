import { GlassPanel, Badge, EmptyState, LoadingState, SplitPane, DataList, useLocale } from '../index';

// MCP server info type matching the API response
export interface MCPServerInfo {
  name: string;
  command: string;
  args: string[];
  enabled: boolean;
}

interface MCPServersTabProps {
  servers: MCPServerInfo[];
  loading: boolean;
  selectedId: string | null;
  onSelect: (id: string | null) => void;
}

export function MCPServersTab({ servers, loading, selectedId, onSelect }: MCPServersTabProps) {
  const { t } = useLocale();

  const items = servers.map((server) => ({
    id: server.name,
    title: server.name,
    subtitle: server.enabled ? 'Enabled' : 'Disabled',
    badge: <Badge variant={server.enabled ? 'success' : 'default'}>{server.enabled ? 'Enabled' : 'Disabled'}</Badge>,
  }));

  const selectedServer = servers.find((s) => s.name === selectedId);

  return (
    <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
    <SplitPane
      masterWidth="360px"
      master={
        <div style={{ display: 'flex', flexDirection: 'column', minHeight: 0, height: '100%' }}>
          <div style={{ padding: '1rem 1.25rem', borderBottom: '1px solid var(--color-border)', flexShrink: 0 }}>
            <h2 style={{ margin: 0, fontSize: '0.9375rem', fontWeight: 800, letterSpacing: '-0.02em' }}>
              {t('portal.mcp')}
            </h2>
          </div>
          <div style={{ flex: 1, overflowY: 'auto', minHeight: 0 }}>
            {loading ? (
              <div style={{ padding: '1.5rem', textAlign: 'center' }}>
                <LoadingState label="Loading servers…" variant="inline" />
              </div>
            ) : servers.length === 0 ? (
              <div style={{ padding: '1.5rem', textAlign: 'center' }}>
                <p style={{ color: 'var(--color-text-dim)', fontSize: '0.875rem' }}>No MCP servers configured</p>
              </div>
            ) : (
              <DataList items={items} selectedId={selectedId} onSelect={onSelect} selectionLabel="server" />
            )}
          </div>
        </div>
      }
      detail={
        selectedServer ? (
          <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
            <header style={{ padding: '1.25rem 1.5rem', borderBottom: '1px solid var(--color-border)', flexShrink: 0 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem' }}>
                <span style={{ fontSize: '1.25rem' }}>🔌</span>
                <h2 style={{ margin: 0, fontSize: '1rem', fontWeight: 700, letterSpacing: '-0.01em' }}>{selectedServer.name}</h2>
                <Badge variant={selectedServer.enabled ? 'success' : 'default'}>
                  {selectedServer.enabled ? 'Enabled' : 'Disabled'}
                </Badge>
              </div>
              <code style={{ fontSize: '11px', color: 'var(--color-text-dim)', fontFamily: 'var(--font-mono)' }}>
                {selectedServer.command}
                {selectedServer.args.length > 0 && ` ${selectedServer.args.join(' ')}`}
              </code>
            </header>
            <div style={{ flex: 1, overflow: 'auto', padding: '1.5rem', display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
              <div>
                <p className="section-label" style={{ marginBottom: '0.5rem' }}>Command</p>
                <GlassPanel style={{ padding: '0.75rem 1rem' }}>
                  <code style={{ fontSize: '0.8125rem', color: 'var(--color-text)', fontFamily: 'var(--font-mono)' }}>
                    {selectedServer.command}
                  </code>
                </GlassPanel>
              </div>
              {selectedServer.args.length > 0 && (
                <div>
                  <p className="section-label" style={{ marginBottom: '0.5rem' }}>Arguments</p>
                  <GlassPanel style={{ padding: '0.75rem 1rem' }}>
                    <code style={{ fontSize: '0.8125rem', color: 'var(--color-text)', fontFamily: 'var(--font-mono)' }}>
                      {selectedServer.args.join(' ')}
                    </code>
                  </GlassPanel>
                </div>
              )}
              <div>
                <p className="section-label" style={{ marginBottom: '0.5rem' }}>Status</p>
                <p style={{ fontSize: '0.9375rem', color: 'var(--color-text)' }}>
                  This server is currently <strong>{selectedServer.enabled ? 'enabled' : 'disabled'}</strong>.
                </p>
              </div>
            </div>
          </div>
        ) : (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--color-text-dim)', fontSize: '0.875rem' }}>
            Select a server to view details
          </div>
        )
      }
    />
    </div>
  );
}