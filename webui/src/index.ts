// ============================================
// GLIMPSE UI — Component Library Root
// Obsidian Glass: Dark-first glassmorphism UI framework
// ============================================

// ── Types ───────────────────────────────────
export type {
  Theme,
  ColorVariant,
  ToastMessage,
  ConfirmOptions,
  CollapsibleVariant,
  SessionEventKind,
  SessionEvent,
  ApiStatus,
  PaginatedResponse,
  FieldConfig,
  LoadingVariant,
  BadgeSize,
} from './types';

// ── Hooks ───────────────────────────────────
export { useTheme } from './hooks/useTheme';
export { useConfirm } from './hooks/useConfirm';
export { useToast } from './hooks/useToast';
export { useApi, useStats, useSessions, useSessionStream, killSession, apiPost, apiGet } from './hooks/useApi';
export type { SessionMetadata, Stats } from './hooks/useApi';

// ── i18n ────────────────────────────────────
export { LocaleProvider, useLocale } from './i18n/locale-context';
export { createTranslator, assertKeyParity } from './i18n/translate';
export type { Locale, Translator, Messages } from './i18n/translate';

// ── UI Primitives ───────────────────────────
export { Button, type ButtonProps } from './components/ui-primitives/Button';
export { Badge, type BadgeProps } from './components/ui-primitives/Badge';
export { InfoTip, type InfoTipProps } from './components/ui-primitives/InfoTip';
export { FieldLabel, type FieldLabelProps } from './components/ui-primitives/FieldLabel';
export { IconButton, type IconButtonProps } from './components/ui-primitives/IconButton';
export {
  FormField,
  SelectField,
  TextField,
  type FormFieldProps,
  type SelectFieldProps,
  type SelectOption,
  type TextFieldProps,
} from './components/ui-primitives/FormField';

// ── Layout ──────────────────────────────────
export {
  PageShell,
  EmptyState,
  LoadingState,
  ErrorBanner,
  type PageShellProps,
  type EmptyStateProps,
  type LoadingStateProps,
  type ErrorBannerProps,
} from './components/layout/PageShell';

export { Card, GlassPanel, type CardProps, type GlassPanelProps } from './components/layout/Card';

export {
  CollapsibleContentBlock,
  type CollapsibleContentBlockProps,
} from './components/layout/Collapsible';

export { Header, type HeaderProps, type NavItem } from './components/layout/Header';

export { SplitPane, type SplitPaneProps } from './components/layout/SplitPane';

export { AppHeader, type AppHeaderProps, type NavTab } from './components/layout/AppHeader';

// ── Feedback ────────────────────────────────
export { Portal, type PortalProps } from './components/feedback/Portal';
export { ConfirmProvider } from './components/feedback/Confirm';
export { ToastProvider } from './components/feedback/ToastDisplay';
export { MutationFeedback, type MutationFeedbackProps } from './components/feedback/MutationFeedback';
export { SettingsDialog, useSettings, type PortalSettings } from './components/feedback/SettingsDialog';

// ── Data Display ────────────────────────────
export {
  SessionEventsPanel,
  type SessionEventsPanelProps,
} from './components/data-display/SessionEventsPanel';

export { StreamPanel, type StreamPanelProps } from './components/data-display/StreamPanel';

export {
  DataList,
  type DataListProps,
  type DataListItem,
} from './components/data-display/DataList';

export { StatCard, type StatCardProps } from './components/data-display/StatCard';

export {
  BriefingCard,
  type BriefingCardProps,
  type BriefingStatus,
} from './components/data-display/BriefingCard';

export { TaskInput, type TaskInputProps } from './components/data-display/TaskInput';

export {
  CronEditor,
  type CronEditorProps,
  type CronEntryData,
} from './components/data-display/CronEditor';

export {
  SourceEditor,
  type SourceEditorProps,
  type SourceEntryData,
  type SourceType,
} from './components/data-display/SourceEditor';