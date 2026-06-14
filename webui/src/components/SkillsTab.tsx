import { useState } from 'react';
import { GlassPanel, Badge, EmptyState, LoadingState, SplitPane, DataList, useLocale } from '../index';

// Skill info type matching the API response
export interface SkillInfo {
  name: string;
  description: string;
  path: string;
  activation_glob?: string;
}

interface SkillsTabProps {
  skills: SkillInfo[];
  loading: boolean;
  selectedId: string | null;
  onSelect: (id: string | null) => void;
}

export function SkillsTab({ skills, loading, selectedId, onSelect }: SkillsTabProps) {
  const { t } = useLocale();

  const items = skills.map((skill) => ({
    id: skill.name,
    title: skill.name,
    subtitle: skill.description || '(no description)',
    badge: <Badge variant="success">Installed</Badge>,
  }));

  const selectedSkill = skills.find((s) => s.name === selectedId);

  return (
    <SplitPane
      masterWidth="360px"
      master={
        <div style={{ display: 'flex', flexDirection: 'column', minHeight: 0, height: '100%' }}>
          <div style={{ padding: '1rem 1.25rem', borderBottom: '1px solid var(--color-border)', flexShrink: 0 }}>
            <h2 style={{ margin: 0, fontSize: '0.9375rem', fontWeight: 800, letterSpacing: '-0.02em' }}>
              {t('portal.skills')}
            </h2>
          </div>
          <div style={{ flex: 1, overflowY: 'auto', minHeight: 0 }}>
            {loading ? (
              <div style={{ padding: '1.5rem', textAlign: 'center' }}>
                <LoadingState label="Loading skills…" variant="inline" />
              </div>
            ) : skills.length === 0 ? (
              <div style={{ padding: '1.5rem', textAlign: 'center' }}>
                <p style={{ color: 'var(--color-text-dim)', fontSize: '0.875rem' }}>No skills installed</p>
              </div>
            ) : (
              <DataList items={items} selectedId={selectedId} onSelect={onSelect} selectionLabel="skill" />
            )}
          </div>
        </div>
      }
      detail={
        selectedSkill ? (
          <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
            <header style={{ padding: '1.25rem 1.5rem', borderBottom: '1px solid var(--color-border)', flexShrink: 0 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem' }}>
                <span style={{ fontSize: '1.25rem' }}>⚡</span>
                <h2 style={{ margin: 0, fontSize: '1rem', fontWeight: 700, letterSpacing: '-0.01em' }}>{selectedSkill.name}</h2>
                <Badge variant="success">Installed</Badge>
              </div>
              <code style={{ fontSize: '11px', color: 'var(--color-text-dim)', fontFamily: 'var(--font-mono)' }}>
                {selectedSkill.path}
              </code>
            </header>
            <div style={{ flex: 1, overflow: 'auto', padding: '1.5rem', display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
              <div>
                <p className="section-label" style={{ marginBottom: '0.5rem' }}>Description</p>
                <p style={{ color: 'var(--color-text)', fontSize: '0.9375rem', lineHeight: 1.6 }}>
                  {selectedSkill.description || '(no description)'}
                </p>
              </div>
              {selectedSkill.activation_glob && (
                <div>
                  <p className="section-label" style={{ marginBottom: '0.5rem' }}>Activation Glob</p>
                  <GlassPanel style={{ padding: '0.75rem 1rem' }}>
                    <code style={{ fontSize: '0.8125rem', color: 'var(--color-text)', fontFamily: 'var(--font-mono)' }}>
                      {selectedSkill.activation_glob}
                    </code>
                  </GlassPanel>
                </div>
              )}
            </div>
          </div>
        ) : (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--color-text-dim)', fontSize: '0.875rem' }}>
            Select a skill to view details
          </div>
        )
      }
    />
  );
}