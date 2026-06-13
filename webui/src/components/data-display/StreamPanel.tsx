import { useEffect, useRef, useState, useCallback } from 'react';
import { CollapsibleContentBlock } from '../layout';
import type { CollapsibleVariant } from '../../types';

// ── StreamPanel ─────────────────────────────

export interface StreamPanelProps {
  title: string;
  sessionId?: string;
  stream?: 'stdout' | 'stderr' | 'transcript';
  isRunning: boolean;
  variant?: 'muted' | 'danger' | 'default';
  fetchStream?: (sessionId: string, stream: 'stdout' | 'stderr' | 'transcript', signal: AbortSignal) => Promise<string>;
  className?: string;
}

/**
 * StreamPanel — Real-time stdout/stderr stream viewer
 * Polls every 1.5s while open and running, auto-scrolls to bottom
 *
 * @example
 * <StreamPanel
 *   title="Output"
 *   sessionId="sess-123"
 *   stream="stdout"
 *   isRunning={true}
 *   fetchStream={async (id, s) => fetch(`/api/sessions/${id}/stream/${s}`).then(r => r.text())}
 * />
 */
export function StreamPanel({
  title,
  sessionId,
  stream,
  isRunning,
  variant = 'default',
  fetchStream,
  className = '',
}: StreamPanelProps) {
  const [content, setContent] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [isOpen, setIsOpen] = useState(true);
  const abortRef = useRef<AbortController | null>(null);
  const bottomRef = useRef<HTMLDivElement | null>(null);

  const variantMap: Record<typeof variant, CollapsibleVariant> = {
    muted: 'muted',
    danger: 'danger',
    default: 'default',
  };

  const poll = useCallback(async () => {
    if (!fetchStream || !sessionId) return;

    abortRef.current = new AbortController();
    setIsLoading(true);
    setError(null);

    try {
      const text = await fetchStream(sessionId, stream ?? 'stdout', abortRef.current.signal);
      setContent((prev) => {
        // Incremental: only append new content
        if (text.startsWith(prev)) return text;
        return prev + text.slice(prev.length);
      });
    } catch (e) {
      if ((e as Error).name !== 'AbortError') {
        setError((e as Error).message);
      }
    } finally {
      setIsLoading(false);
    }
  }, [sessionId, stream, fetchStream]);

  // Poll while running and open
  useEffect(() => {
    if (!isRunning || !isOpen) return;

    poll();
    const interval = setInterval(poll, 1500);
    return () => {
      clearInterval(interval);
      abortRef.current?.abort();
    };
  }, [isRunning, isOpen, poll]);

  // Auto-scroll to bottom
  useEffect(() => {
    if (isOpen) {
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
    }
  }, [content, isOpen]);

  const lineCount = content.split('\n').length;
  const isTruncated = content.includes('…') || content.includes('[truncated]');

  return (
    <CollapsibleContentBlock
      title={
        <span style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          {title}
          {isRunning && (
            <span className="live-indicator" style={{ color: 'var(--color-primary)' }}>
              ●
            </span>
          )}
          <span
            style={{
              fontSize: '0.6875rem',
              color: 'var(--color-text-dim)',
              fontFamily: 'var(--font-mono)',
            }}
          >
            {lineCount}L
          </span>
          {isTruncated && (
            <span style={{ fontSize: '0.6875rem', color: 'var(--color-warning)' }}>truncated</span>
          )}
        </span>
      }
      meta={error ? `Error: ${error}` : undefined}
      variant={error ? 'danger' : variantMap[variant]}
      defaultCollapsed={false}
      className={className}
    >
      {error && (
        <div
          style={{
            fontSize: '0.75rem',
            color: 'var(--color-danger)',
            marginBottom: '0.5rem',
            fontFamily: 'var(--font-mono)',
          }}
        >
          {error}
        </div>
      )}

      {isLoading && !content && (
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '0.5rem',
            fontSize: '0.8125rem',
            color: 'var(--color-text-dim)',
          }}
        >
          <span
            className="spinner"
            style={{ width: '12px', height: '12px', borderWidth: '1.5px' }}
          />
          Waiting for output…
        </div>
      )}

      {!content && !isLoading && !error && (
        <div
          style={{
            fontSize: '0.8125rem',
            color: 'var(--color-text-dim)',
            fontStyle: 'italic',
          }}
        >
          No output yet
        </div>
      )}

      {content && (
        <pre
          style={{
            fontSize: '0.75rem',
            lineHeight: 1.6,
            fontFamily: 'var(--font-mono)',
            color: variant === 'danger' ? 'var(--color-danger)' : 'var(--color-text)',
            background: 'var(--color-glass-subtle)',
            padding: '0.75rem',
            borderRadius: '8px',
            overflow: 'auto',
            maxHeight: '400px',
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-all',
          }}
        >
          {content}
          {isRunning && (
            <span style={{ opacity: 0.6 }}>{'\n'}█{/* cursor blink */}</span>
          )}
          <div ref={bottomRef} />
        </pre>
      )}
    </CollapsibleContentBlock>
  );
}