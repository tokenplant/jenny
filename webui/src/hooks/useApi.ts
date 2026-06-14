import { useState, useEffect, useCallback } from 'react';

// Types for API responses
export interface SessionMetadata {
  id: string;
  status: 'running' | 'exited';
  pid?: number;
  cwd: string;
  model: string;
  start_time: number;
  end_time?: number;
  total_tokens?: number;
  total_cost?: number;
}

export interface Stats {
  total_sessions: number;
  active_sessions: number;
  total_cost_usd: number;
  total_tokens: number;
}

// Get portal config from URL
function getPortalConfig() {
  if (typeof window === 'undefined') {
    return { apiUrl: '', token: '' };
  }

  const params = new URLSearchParams(window.location.search);
  const token = params.get('token') || '';

  // Derive API URL from current origin
  const apiUrl = `${window.location.protocol}//${window.location.host}`;

  return { apiUrl, token };
}

// API fetch wrapper
export async function apiGet<T>(path: string): Promise<T> {
  const { apiUrl, token } = getPortalConfig();
  const separator = path.includes('?') ? '&' : '?';
  const url = `${apiUrl}${path}${separator}token=${token}`;

  const res = await fetch(url);
  if (!res.ok) {
    const error = await res.text();
    throw new Error(error || res.statusText);
  }
  return res.json();
}
// API POST wrapper
export async function apiPost<T>(path: string, body: unknown): Promise<T> {
  const { apiUrl, token } = getPortalConfig();
  const url = `${apiUrl}${path}?token=${token}`;

  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const error = await res.text();
    throw new Error(error || res.statusText);
  }
  return res.json();
}

// useApi hook for fetching data
export function useApi<T>(path: string, deps: any[] = []) {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await apiGet<T>(path);
      setData(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, ...deps]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  return { data, loading, error, refetch: fetchData };
}

// useStats hook
export function useStats() {
  return useApi<Stats>('/api/stats');
}

// useSessions hook
export function useSessions() {
  return useApi<SessionMetadata[]>('/api/sessions');
}

// useSessionStream hook for SSE
export function useSessionStream(sessionId: string, onMessage: (data: any) => void) {
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    if (!sessionId) return;

    const { apiUrl, token } = getPortalConfig();
    const url = `${apiUrl}/api/sessions/${sessionId}/stream?token=${token}`;

    const es = new EventSource(url);

    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);

    es.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        onMessage(data);
      } catch {
        // Ignore parse errors
      }
    };

    return () => {
      es.close();
      setConnected(false);
    };
  }, [sessionId, onMessage]);

  return { connected };
}

// Kill session action
export async function killSession(sessionId: string): Promise<void> {
  const { apiUrl, token } = getPortalConfig();
  const url = `${apiUrl}/api/sessions/${sessionId}/kill?token=${token}`;

  const res = await fetch(url, { method: 'POST' });
  if (!res.ok) {
    throw new Error(await res.text());
  }
}