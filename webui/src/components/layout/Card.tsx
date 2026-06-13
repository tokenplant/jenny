import { type ReactNode } from 'react';

// ── Card ────────────────────────────────────

export interface CardProps {
  children: ReactNode;
  className?: string;
  padding?: boolean; // apply glass-panel padding
  glow?: 'primary' | 'accent' | 'danger' | 'success' | false;
  onClick?: () => void;
  interactive?: boolean; // hover lift effect
}

/**
 * Card — Glass surface container
 * The primary content container for the design system
 *
 * @example
 * <Card glow="primary" interactive>
 *   <h3>Card Title</h3>
 *   <p>Card content goes here</p>
 * </Card>
 */
export function Card({
  children,
  className = '',
  padding = false,
  glow = false,
  onClick,
  interactive = false,
}: CardProps) {
  const glowClass = glow ? `glow-${glow}` : '';

  return (
    <div
      className={[
        'glass',
        glowClass,
        interactive ? 'hover-lift' : '',
        className,
      ].filter(Boolean).join(' ')}
      style={padding ? { padding: '1.25rem' } : undefined}
      onClick={onClick}
      role={onClick ? 'button' : undefined}
      tabIndex={onClick ? 0 : undefined}
      onKeyDown={onClick ? (e) => e.key === 'Enter' && onClick() : undefined}
    >
      {children}
    </div>
  );
}

// ── GlassPanel ──────────────────────────────

export interface GlassPanelProps {
  children: ReactNode;
  className?: string;
  style?: React.CSSProperties;
  interactive?: boolean; // hover lift effect
  onClick?: () => void;
}

/**
 * GlassPanel — Padded glass container (convenience wrapper)
 */
export function GlassPanel({ children, className = '', style, interactive = false, onClick }: GlassPanelProps) {
  return (
    <div className={['glass', 'glass-panel', interactive ? 'hover-lift' : '', className].filter(Boolean).join(' ')} style={style} onClick={onClick} role={onClick ? 'button' : undefined} tabIndex={onClick ? 0 : undefined} onKeyDown={onClick ? (e) => e.key === 'Enter' && onClick() : undefined}>
      {children}
    </div>
  );
}