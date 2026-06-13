import { FieldLabel } from './FieldLabel';

// ── FormField ───────────────────────────────

export interface FormFieldProps {
  label: string;
  tooltip?: string;
  optional?: boolean;
  children: React.ReactNode;
  className?: string;
}

export function FormField({ label, tooltip, optional, children, className = '' }: FormFieldProps) {
  return (
    <div className={className} style={{ display: 'flex', flexDirection: 'column', gap: '0.375rem' }}>
      <FieldLabel tooltip={tooltip} optional={optional}>
        {label}
      </FieldLabel>
      {children}
    </div>
  );
}

// ── SelectField ─────────────────────────────

export interface SelectOption {
  value: string;
  label: string;
  disabled?: boolean;
}

export interface SelectFieldProps {
  id?: string;
  value: string;
  onChange: (value: string) => void;
  options: SelectOption[];
  placeholder?: string;
  disabled?: boolean;
  className?: string;
}

/**
 * SelectField — Native <select> with custom chevron overlay.
 * appearance: none hides the native arrow; custom SVG chevron replaces it.
 * Focus is handled via onFocus/onBlur (not .focus-ring class).
 */
export function SelectField({
  id,
  value,
  onChange,
  options,
  placeholder,
  disabled = false,
  className = '',
}: SelectFieldProps) {
  return (
    <div style={{ position: 'relative', width: '100%' }}>
      <select
        id={id}
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        className={className}
        onFocus={(e) => {
          e.currentTarget.style.borderColor = 'var(--color-primary)';
          e.currentTarget.style.boxShadow = '0 0 0 2px var(--color-primary-glow)';
        }}
        onBlur={(e) => {
          e.currentTarget.style.borderColor = 'var(--color-border)';
          e.currentTarget.style.boxShadow = 'none';
        }}
        style={{
          width: '100%',
          padding: '0.5rem 2.25rem 0.5rem 0.75rem',
          background: 'var(--color-surface-alt)',
          border: '1px solid var(--color-border)',
          borderRadius: '10px',
          fontSize: '0.875rem',
          color: 'var(--color-text)',
          cursor: disabled ? 'not-allowed' : 'pointer',
          opacity: disabled ? 0.5 : 1,
          appearance: 'none',
          WebkitAppearance: 'none',
          outline: 'none',
          transition: 'border-color 0.15s, box-shadow 0.15s',
        }}
      >
        {placeholder && (
          <option value="" disabled>
            {placeholder}
          </option>
        )}
        {options.map((opt) => (
          <option key={opt.value} value={opt.value} disabled={opt.disabled}>
            {opt.label}
          </option>
        ))}
      </select>

      {/* Custom chevron — replaces native arrow (appearance: none) */}
      <span
        aria-hidden="true"
        style={{
          position: 'absolute',
          right: '0.625rem',
          top: '50%',
          transform: 'translateY(-50%)',
          pointerEvents: 'none',
          color: 'var(--color-text-muted)',
          display: 'flex',
          alignItems: 'center',
        }}
      >
        <svg width="10" height="10" viewBox="0 0 10 10" fill="none" xmlns="http://www.w3.org/2000/svg">
          <path d="M5 7L1.5 3.5H8.5L5 7Z" fill="currentColor" />
        </svg>
      </span>
    </div>
  );
}

// ── TextField ─────────────────────────────

export interface TextFieldProps {
  value: string;
  onChange: (value: string) => void;
  className?: string;
  style?: React.CSSProperties;
  multiline?: boolean;
  rows?: number;
  placeholder?: string;
  disabled?: boolean;
  autoFocus?: boolean;
  type?: string;
}

/**
 * TextField — Styled text input or textarea.
 * Focus: border turns primary + box-shadow glow.
 */
export function TextField({
  value,
  onChange,
  className = '',
  multiline = false,
  rows = 3,
  style,
  placeholder,
  disabled,
  autoFocus,
  type,
}: TextFieldProps) {
  const baseStyle: React.CSSProperties = {
    padding: '0.5rem 0.75rem',
    background: 'var(--color-surface-alt)',
    border: '1px solid var(--color-border)',
    borderRadius: '10px',
    fontSize: '0.875rem',
    color: 'var(--color-text)',
    outline: 'none',
    width: '100%',
    transition: 'border-color 0.15s, box-shadow 0.15s',
    ...style,
  };

  if (multiline) {
    const handleFocus = (e: React.FocusEvent<HTMLTextAreaElement>) => {
      e.currentTarget.style.borderColor = 'var(--color-primary)';
      e.currentTarget.style.boxShadow = '0 0 0 2px var(--color-primary-glow)';
    };
    const handleBlur = (e: React.FocusEvent<HTMLTextAreaElement>) => {
      e.currentTarget.style.borderColor = 'var(--color-border)';
      e.currentTarget.style.boxShadow = 'none';
    };
    return (
      <textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={className}
        rows={rows}
        placeholder={placeholder}
        disabled={disabled}
        autoFocus={autoFocus}
        onFocus={handleFocus}
        onBlur={handleBlur}
        style={{
          ...baseStyle,
          resize: 'vertical',
          minHeight: '80px',
        }}
      />
    );
  }

  return (
    <input
      type={type}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className={className}
      placeholder={placeholder}
      disabled={disabled}
      autoFocus={autoFocus}
      onFocus={(e) => {
        e.currentTarget.style.borderColor = 'var(--color-primary)';
        e.currentTarget.style.boxShadow = '0 0 0 2px var(--color-primary-glow)';
      }}
      onBlur={(e) => {
        e.currentTarget.style.borderColor = 'var(--color-border)';
        e.currentTarget.style.boxShadow = 'none';
      }}
      style={baseStyle}
    />
  );
}