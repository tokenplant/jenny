import { useState } from 'react';
import { IconButton, Button } from '../ui-primitives';
import type { Theme } from '../../types';

// ── Header ──────────────────────────────────

export interface NavItem {
  id: string;
  label: string;
  icon?: string;
}

export interface HeaderProps {
  logo?: React.ReactNode;
  title?: string;
  navItems?: NavItem[];
  activeNav?: string;
  onNavChange?: (id: string) => void;
  locale?: string;
  onLocaleChange?: (locale: string) => void;
  theme?: Theme;
  onThemeChange?: (theme: Theme) => void;
  actions?: React.ReactNode;
}

/**
 * Header — Sticky app header with navigation
 *
 * @example
 * <Header
 *   title="My App"
 *   navItems={[{ id: 'home', label: 'Home' }, { id: 'settings', label: 'Settings' }]}
 *   activeNav="home"
 *   onNavChange={(id) => setActive(id)}
 * />
 */
export function Header({
  logo,
  title,
  navItems = [],
  activeNav,
  onNavChange,
  locale,
  onLocaleChange,
  theme,
  onThemeChange,
  actions,
}: HeaderProps) {
  return (
    <header
      className="app-header glass"
      style={{
        position: 'sticky',
        top: 0,
        zIndex: 40,
        display: 'flex',
        alignItems: 'center',
        padding: '0 1.5rem',
        height: '56px',
        gap: '1.5rem',
        borderRadius: 0, // full-width header
        borderTop: 'none',
        borderLeft: 'none',
        borderRight: 'none',
      }}
    >
      {/* Logo + Title */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexShrink: 0 }}>
        {logo}
        {title && (
          <span
            style={{
              fontSize: '1rem',
              fontWeight: 800,
              letterSpacing: '-0.02em',
              color: 'var(--color-text)',
            }}
          >
            {title}
          </span>
        )}
      </div>

      {/* Nav */}
      {navItems.length > 0 && (
        <nav style={{ display: 'flex', alignItems: 'center', gap: '0.25rem', flex: 1 }}>
          {navItems.map((item) => (
            <button
              key={item.id}
              type="button"
              className="focus-ring"
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '0.375rem',
                padding: '0.375rem 0.75rem',
                background: activeNav === item.id ? 'var(--color-glass-subtle)' : 'none',
                border: 'none',
                borderRadius: '8px',
                fontSize: '0.875rem',
                fontWeight: activeNav === item.id ? 600 : 400,
                color:
                  activeNav === item.id
                    ? 'var(--color-text)'
                    : 'var(--color-text-muted)',
                cursor: 'pointer',
                transition: 'all 0.2s',
              }}
              onClick={() => onNavChange?.(item.id)}
              aria-current={activeNav === item.id ? 'page' : undefined}
            >
              {item.icon && <span>{item.icon}</span>}
              {item.label}
            </button>
          ))}
        </nav>
      )}

      {/* End actions */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginLeft: 'auto' }}>
        {actions}

        {/* Locale switcher */}
        {onLocaleChange && locale && (
          <button
            type="button"
            className="focus-ring glass-subtle"
            style={{
              padding: '0.25rem 0.5rem',
              border: 'none',
              borderRadius: '6px',
              fontSize: '0.75rem',
              fontWeight: 600,
              color: 'var(--color-text-muted)',
              cursor: 'pointer',
            }}
            onClick={() =>
              onLocaleChange(locale === 'en' ? 'zh-Hans' : 'en')
            }
          >
            {locale === 'en' ? '中文' : 'EN'}
          </button>
        )}

        {/* Theme switcher */}
        {onThemeChange && theme && (
          <div style={{ display: 'flex', gap: '2px' }}>
            {(['light', 'system', 'dark'] as Theme[]).map((t) => (
              <button
                key={t}
                type="button"
                className="focus-ring"
                style={{
                  padding: '0.25rem 0.5rem',
                  background: theme === t ? 'var(--color-glass-subtle)' : 'none',
                  border: 'none',
                  borderRadius: '6px',
                  fontSize: '0.6875rem',
                  fontWeight: 600,
                  color: theme === t ? 'var(--color-text)' : 'var(--color-text-dim)',
                  cursor: 'pointer',
                  textTransform: 'capitalize',
                }}
                onClick={() => onThemeChange(t)}
                aria-pressed={theme === t}
              >
                {t === 'system' ? '◑' : t === 'light' ? '☀' : '☾'}
              </button>
            ))}
          </div>
        )}
      </div>
    </header>
  );
}