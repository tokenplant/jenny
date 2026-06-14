import { Button } from '../../ui-primitives';

interface MarketplaceURLBarProps {
  sourceUrl: string;
  onSourceUrlChange: (url: string) => void;
  onBrowse: () => void;
  onReset: () => void;
  isDefaultUrl: boolean;
  loading?: boolean;
}

export function MarketplaceURLBar({
  sourceUrl,
  onSourceUrlChange,
  onBrowse,
  onReset,
  isDefaultUrl,
  loading,
}: MarketplaceURLBarProps) {
  return (
    <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
      <input
        type="text"
        value={sourceUrl}
        onChange={e => onSourceUrlChange(e.target.value)}
        onKeyDown={e => e.key === 'Enter' && onBrowse()}
        aria-label="Marketplace URL"
        className="focus-ring"
        style={{
          flex: 1,
          padding: '0.5rem 0.75rem',
          borderRadius: '10px',
          border: '1px solid var(--color-border)',
          background: 'var(--color-surface-alt)',
          color: 'var(--color-text)',
          fontFamily: 'var(--font-mono)',
          fontSize: '0.8125rem',
          outline: 'none',
        }}
        placeholder="https://raw.githubusercontent.com/.../index.json"
      />
      <Button variant="primary" size="sm" onClick={onBrowse} disabled={loading}>
        Browse
      </Button>
      {!isDefaultUrl && (
        <Button variant="ghost" size="sm" onClick={onReset}>
          Reset
        </Button>
      )}
    </div>
  );
}