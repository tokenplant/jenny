import { useCallback } from 'react';
import type { Theme } from '../../types';
import type { Locale } from '../../i18n/translate';

// ── AppHeader ───────────────────────────────

export interface NavTab {
  id: string;
  label: string;
  href?: string;       // optional external link
  onClick?: () => void;
}

export interface AppHeaderProps {
  /** Brand name text */
  brand?: string;
  /** Show brand dot (default true) */
  showBrandDot?: boolean;
  /** Navigation tabs */
  tabs?: NavTab[];
  /** Currently active tab id */
  activeTab?: string | null;
  /** Called when tab changes (for client-side routing) */
  onTabChange?: (id: string) => void;
  /** Max width (default '72rem' = max-w-6xl) */
  maxWidth?: string;
  /** Locale switcher */
  locale?: Locale;
  onLocaleChange?: (locale: Locale) => void;
  /** Theme switcher */
  theme?: Theme;
  onThemeChange?: (theme: Theme) => void;
  /** Custom end content */
  actions?: React.ReactNode;
}

/**
 * AppHeader — Full application header with brand, tab navigation, and controls
 * Matches Glimpse's exact header design: centered max-width, tab nav, locale/theme switchers
 *
 * @example
 * <AppHeader
 *   brand="MyApp"
 *   tabs={[
 *     { id: 'home', label: 'Home' },
 *     { id: 'settings', label: 'Settings' },
 *   ]}
 *   activeTab="home"
 *   onTabChange={setActiveTab}
 *   theme={theme}
 *   onThemeChange={setTheme}
 *   locale={locale}
 *   onLocaleChange={setLocale}
 * />
 */
export function AppHeader({
  brand,
  showBrandDot = true,
  tabs = [],
  activeTab,
  onTabChange,
  maxWidth = '72rem',
  locale,
  onLocaleChange,
  theme,
  onThemeChange,
  actions,
}: AppHeaderProps) {
  const handleTabClick = useCallback(
    (tab: NavTab, e: React.MouseEvent) => {
      // Allow ctrl/meta/shift-click and middle-click for native link behavior
      if (e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey) {
        e.preventDefault();
        tab.onClick?.();
        onTabChange?.(tab.id);
      }
    },
    [onTabChange]
  );

  return (
    <>
      {/* Ambient background blobs */}
      <div
        style={{
          position: 'fixed',
          inset: 0,
          pointerEvents: 'none',
          overflow: 'hidden',
          zIndex: 0,
        }}
        aria-hidden="true"
      >
        <div
          style={{
            position: 'absolute',
            top: '-10%',
            left: '-10%',
            width: '40%',
            height: '40%',
            borderRadius: '50%',
            background: 'oklch(0.55 0.18 285 / 0.05)',
            filter: 'blur(120px)',
          }}
        />
        <div
          style={{
            position: 'absolute',
            bottom: '-10%',
            right: '-10%',
            width: '40%',
            height: '40%',
            borderRadius: '50%',
            background: 'oklch(0.65 0.12 160 / 0.05)',
            filter: 'blur(120px)',
          }}
        />
      </div>

      <header
        className="glass sticky"
        style={{
          top: '1rem',
          zIndex: 40,
          margin: '0 auto',
          width: `calc(100% - 3rem)`,
          maxWidth,
          padding: '0.625rem 1.5rem',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          borderRadius: '16px',
        }}
      >
        {/* Left: Brand + Tabs */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '2.5rem' }}>
          {/* Brand */}
          {(brand || showBrandDot) && (
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.625rem', flexShrink: 0 }}>
              {showBrandDot && (
                <div
                  style={{
                    width: '6px',
                    height: '6px',
                    borderRadius: '50%',
                    background: 'var(--color-primary)',
                    boxShadow: '0 0 10px var(--color-primary)',
                    flexShrink: 0,
                  }}
                />
              )}
              {brand && (
                <h1
                  style={{
                    fontSize: '13px',
                    fontWeight: 900,
                    textTransform: 'uppercase',
                    letterSpacing: '0.15em',
                    color: 'var(--color-text)',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {brand}
                </h1>
              )}
            </div>
          )}

          {/* Tab Navigation */}
          {tabs.length > 0 && (
            <nav aria-label="Main navigation" style={{ display: 'flex', gap: '0.25rem' }}>
              {tabs.map((tab) => {
                const isActive = tab.id === activeTab;
                return (
                  <a
                    key={tab.id}
                    href={tab.href ?? `#${tab.id}`}
                    onClick={(e) => handleTabClick(tab, e)}
                    aria-current={isActive ? 'page' : undefined}
                    className="focus-ring"
                    style={{
                      padding: '0.375rem 0.875rem',
                      fontSize: '11px',
                      fontWeight: 700,
                      textTransform: 'uppercase',
                      letterSpacing: '0.1em',
                      borderRadius: '8px',
                      transition: 'all 0.3s',
                      color: isActive ? 'var(--color-primary)' : 'var(--color-text-muted)',
                      background: isActive ? 'oklch(0.55 0.18 285 / 0.1)' : 'transparent',
                      boxShadow: isActive ? 'inset 0 0 12px var(--color-primary-glow)' : 'none',
                      textDecoration: 'none',
                      whiteSpace: 'nowrap',
                    }}
                    onMouseEnter={(e) => {
                      if (!isActive) {
                        (e.currentTarget as HTMLElement).style.color = 'var(--color-text)';
                        (e.currentTarget as HTMLElement).style.background = 'var(--color-glass-hover)';
                      }
                    }}
                    onMouseLeave={(e) => {
                      if (!isActive) {
                        (e.currentTarget as HTMLElement).style.color = 'var(--color-text-muted)';
                        (e.currentTarget as HTMLElement).style.background = 'transparent';
                      }
                    }}
                  >
                    {tab.label}
                  </a>
                );
              })}
            </nav>
          )}
        </div>

        {/* Right: Controls */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          {actions}

          {/* Locale Switcher */}
          {onLocaleChange && locale && (
            <LocaleSwitcher locale={locale} onLocaleChange={onLocaleChange} />
          )}

          {/* Theme Switcher */}
          {onThemeChange && theme && (
            <ThemeSwitcher theme={theme} onThemeChange={onThemeChange} />
          )}
        </div>
      </header>
    </>
  );
}

// ── LocaleSwitcher ─────────────────────────

function LocaleSwitcher({
  locale,
  onLocaleChange,
}: {
  locale: Locale;
  onLocaleChange: (locale: Locale) => void;
}) {
  return (
    <div
      role="group"
      aria-label="Language"
      style={{
        display: 'flex',
        alignItems: 'center',
        padding: '2px',
        gap: '2px',
        border: '1px solid var(--color-border)',
        borderRadius: '8px',
        background: 'var(--color-glass-subtle)',
        flexShrink: 0,
      }}
    >
      {([
        { value: 'en' as Locale, label: 'EN' },
        { value: 'zh-Hans' as Locale, label: '中文' },
        { value: 'zh-Hant' as Locale, label: '繁中' },
      ] as { value: Locale; label: string }[]).map((opt) => {
        const active = locale === opt.value;
        return (
          <button
            key={opt.value}
            type="button"
            onClick={() => onLocaleChange(opt.value)}
            aria-pressed={active}
            className="focus-ring"
            style={{
              height: '28px',
              padding: '0 8px',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              borderRadius: '6px',
              border: 'none',
              cursor: 'pointer',
              fontSize: '10px',
              fontWeight: 700,
              textTransform: 'uppercase',
              letterSpacing: '0.08em',
              transition: 'all 0.2s',
              color: active ? 'var(--color-primary)' : 'var(--color-text-dim)',
              background: active ? 'oklch(0.55 0.18 285 / 0.15)' : 'transparent',
            }}
          >
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}

// ── ThemeSwitcher ──────────────────────────

const THEME_OPTIONS: { value: Theme; icon: string }[] = [
  { value: 'light', icon: '☀' },
  { value: 'system', icon: '⊘' },
  { value: 'dark', icon: '☾' },
];

function ThemeSwitcher({
  theme,
  onThemeChange,
}: {
  theme: Theme;
  onThemeChange: (theme: Theme) => void;
}) {
  return (
    <div
      role="group"
      aria-label="Theme"
      style={{
        display: 'flex',
        alignItems: 'center',
        padding: '2px',
        gap: '2px',
        border: '1px solid var(--color-border)',
        borderRadius: '8px',
        background: 'var(--color-glass-subtle)',
        flexShrink: 0,
      }}
    >
      {THEME_OPTIONS.map(({ value, icon }) => {
        const active = theme === value;
        return (
          <button
            key={value}
            type="button"
            onClick={() => onThemeChange(value)}
            aria-label={`Switch to ${value} theme`}
            aria-pressed={active}
            className="focus-ring"
            style={{
              width: '28px',
              height: '28px',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              borderRadius: '6px',
              border: 'none',
              cursor: 'pointer',
              fontSize: '12px',
              transition: 'all 0.2s',
              color: active ? 'var(--color-primary)' : 'var(--color-text-dim)',
              background: active ? 'oklch(0.55 0.18 285 / 0.15)' : 'transparent',
            }}
          >
            {icon}
          </button>
        );
      })}
    </div>
  );
}