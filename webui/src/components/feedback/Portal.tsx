import { useEffect, useState, type ReactNode } from 'react';
import { createPortal } from 'preact/compat';

// ── Portal ──────────────────────────────────

export interface PortalProps {
  children: ReactNode;
  container?: HTMLElement;
}

/**
 * Portal — Render children into a different DOM subtree
 * Handles SSR (returns null when document is undefined)
 *
 * @example
 * <Portal container={document.body}>
 *   <Modal>Content</Modal>
 * </Portal>
 */
export function Portal({ children, container }: PortalProps): ReactNode {
  const [mountNode, setMountNode] = useState<HTMLElement | null>(null);

  useEffect(() => {
    const target = container ?? document.body;
    setMountNode(target);
    return () => {
      // Portal content is cleaned up by React when parent unmounts
    };
  }, [container]);

  if (!mountNode) return null;
  return createPortal(children, mountNode) as ReactNode;
}