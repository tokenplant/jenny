// ── Theme ───────────────────────────────────
export type Theme = 'light' | 'dark' | 'system';

// ── Colors ──────────────────────────────────
export type ColorVariant = 'default' | 'primary' | 'accent' | 'danger' | 'success' | 'warning';

// ── Toast / Notification ────────────────────
export interface ToastMessage {
  id: string;
  kind: 'info' | 'success' | 'warning' | 'error';
  title: string;
  message?: string;
  duration?: number; // ms, 0 = persistent
}

// ── Confirm Dialog ──────────────────────────
export interface ConfirmOptions {
  title: string;
  message?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  dangerous?: boolean;
}

// ── Collapsible ─────────────────────────────
export type CollapsibleVariant = 'default' | 'accent' | 'danger' | 'muted';

// ── Session Events ──────────────────────────
export type SessionEventKind =
  | 'init'
  | 'user'
  | 'assistant'
  | 'tool'
  | 'tool_result'
  | 'thinking'
  | 'result'
  | 'other';

export interface SessionEvent {
  id: string;
  kind: SessionEventKind;
  badge: string;
  preview: string;
  full?: string;
  hasResult?: boolean;
  timestamp_ms?: number;
}

// ── API Response Shapes ─────────────────────
export interface ApiStatus {
  version: string;
  mode: string;
  workspace: string;
}

export interface PaginatedResponse<T> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
  hasMore: boolean;
}

// ── Form Field ──────────────────────────────
export interface FieldConfig {
  label: string;
  tooltip?: string;
  optional?: boolean;
  placeholder?: string;
}

// ── Loading State ───────────────────────────
export type LoadingVariant = 'full' | 'inline' | 'skeleton';

// ── Badge Size ──────────────────────────────
export type BadgeSize = 'sm' | 'md';