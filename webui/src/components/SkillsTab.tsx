import { GlassPanel, Badge, EmptyState, useLocale } from '../index';

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
}

export function SkillsTab({ skills, loading }: SkillsTabProps) {
  const { t } = useLocale();

  if (loading) {
    return (
      <div style={{ padding: '2rem', textAlign: 'center', color: 'var(--color-text-dim)' }}>
        {t('common.loading')}
      </div>
    );
  }

  if (!skills || skills.length === 0) {
    return (
      <div style={{ padding: '4rem 2rem', textAlign: 'center' }}>
        <EmptyState
          title="No skills installed"
          hint="Skills extend jenny's capabilities. Install skills from the Marketplace."
        />
      </div>
    );
  }

  return (
    <div style={{ padding: '2rem', maxWidth: '800px', margin: '0 auto' }}>
      <h2 style={{ marginBottom: '1.5rem' }}>{t('portal.skills')}</h2>
      <div style={{ display: 'grid', gap: '1rem' }}>
        {skills.map((skill) => (
          <GlassPanel key={skill.name} style={{ padding: '1.25rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
              <div style={{ flex: 1, overflow: 'hidden' }}>
                <h3 style={{ margin: 0, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                  {skill.name}
                </h3>
                <p
                  style={{
                    margin: '0.5rem 0',
                    color: 'var(--color-text-muted)',
                    fontSize: '0.875rem',
                    overflow: 'hidden',
                    display: '-webkit-box',
                    WebkitLineClamp: 2,
                    WebkitBoxOrient: 'vertical',
                  }}
                >
                  {skill.description || '(no description)'}
                </p>
                <code
                  style={{
                    fontSize: '11px',
                    color: 'var(--color-text-dim)',
                    display: 'block',
                    whiteSpace: 'nowrap',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                  }}
                >
                  {skill.path}
                </code>
                {skill.activation_glob && (
                  <div style={{ marginTop: '0.5rem', fontSize: '0.75rem', color: 'var(--color-text-muted)' }}>
                    Activates on: <code>{skill.activation_glob}</code>
                  </div>
                )}
              </div>
              <Badge variant="success">Installed</Badge>
            </div>
          </GlassPanel>
        ))}
      </div>
    </div>
  );
}
