---
name: Jenny
description: A calm, luminous control room for orchestrating AI agent workflows
colors:
  surface-frost: "oklch(0.98 0.002 260)"
  surface-obsidian: "oklch(0.08 0.01 260)"
  glass-frost: "oklch(0.99 0.002 260 / 0.7)"
  glass-obsidian: "oklch(0.16 0.01 260 / 0.65)"
  glass-subtle-light: "oklch(1 0 0 / 0.3)"
  glass-subtle-dark: "oklch(1 0 0 / 0.03)"
  text-primary: "oklch(0.2 0.01 260)"
  text-muted: "oklch(0.45 0.01 260)"
  text-dim: "oklch(0.65 0.01 260)"
  text-inverse: "oklch(0.98 0 0)"
  border-soft: "oklch(0 0 0 / 0.1)"
  border-glass: "oklch(0 0 0 / 0.08)"
  violet-signal: "oklch(0.55 0.18 285)"
  violet-signal-dark: "oklch(0.7 0.15 285)"
  teal-beacon: "oklch(0.65 0.12 160)"
  success-moss: "oklch(0.6 0.15 150)"
  warning-amber: "oklch(0.65 0.15 50)"
  danger-ember: "oklch(0.55 0.18 25)"
typography:
  display:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, Segoe UI, Roboto, sans-serif"
    fontSize: "22px"
    fontWeight: 900
    lineHeight: 1.2
    letterSpacing: "-0.035em"
  headline:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, Segoe UI, Roboto, sans-serif"
    fontSize: "18px"
    fontWeight: 700
    lineHeight: 1.2
    letterSpacing: "-0.03em"
  title:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, Segoe UI, Roboto, sans-serif"
    fontSize: "14px"
    fontWeight: 500
    lineHeight: 1.4
    letterSpacing: "normal"
  body:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, Segoe UI, Roboto, sans-serif"
    fontSize: "15px"
    fontWeight: 400
    lineHeight: 1.6
    letterSpacing: "-0.011em"
  label:
    fontFamily: "Inter, -apple-system, BlinkMacSystemFont, Segoe UI, Roboto, sans-serif"
    fontSize: "11px"
    fontWeight: 700
    lineHeight: 1.4
    letterSpacing: "0.05em"
  mono:
    fontFamily: "JetBrains Mono, ui-monospace, monospace"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: "normal"
rounded:
  sm: "8px"
  md: "14px"
  lg: "20px"
  pill: "9999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "16px"
  lg: "28px"
  gutter: "40px"
components:
  button-primary:
    backgroundColor: "oklch(0.55 0.18 285 / 0.2)"
    textColor: "{colors.violet-signal}"
    rounded: "{rounded.sm}"
    padding: "8px 20px"
  button-primary-hover:
    backgroundColor: "oklch(0.55 0.18 285 / 0.3)"
    textColor: "{colors.violet-signal}"
    rounded: "{rounded.sm}"
    padding: "8px 20px"
  button-accent:
    backgroundColor: "oklch(0.65 0.12 160 / 0.2)"
    textColor: "{colors.teal-beacon}"
    rounded: "{rounded.sm}"
    padding: "8px 16px"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.text-muted}"
    rounded: "{rounded.sm}"
    padding: "4px 8px"
  input-field:
    backgroundColor: "oklch(1 0 0 / 0.4)"
    textColor: "{colors.text-primary}"
    rounded: "{rounded.sm}"
    padding: "8px 12px"
  nav-tab-active:
    backgroundColor: "oklch(0.55 0.18 285 / 0.1)"
    textColor: "{colors.violet-signal}"
    rounded: "{rounded.sm}"
    padding: "6px 14px"
  nav-tab-default:
    backgroundColor: "transparent"
    textColor: "{colors.text-muted}"
    rounded: "{rounded.sm}"
    padding: "6px 14px"
---

# Design System: Glimpse

## Overview

**Creative North Star: "The Luminous Control Room"**

Glimpse is a personal AI workstation UI: dense when it needs to be, never loud. The aesthetic is calm and ethereal, with sparkle reserved for moments that matter (an active pipeline, a live stream, a primary action). Surfaces read as frosted glass in light mode and obsidian glass in dark mode; depth comes from translucency, soft ambient shadow, and restrained violet/teal luminosity, not from skeuomorphic relief or saturated fills.

The system rejects generic SaaS dashboards, AI-slop neon, chat-bubble metaphors, flat lifeless panels, neumorphism, and high-saturation accents. Accessibility is part of the visual language: focus rings, semantic color paired with labels/icons, and reduced-motion respect are non-negotiable.

**Key Characteristics:**

- OKLCH-first tokens with cool violet hue (260–285) across neutrals and accents
- Glass panels (20px radius) and glass-subtle containers (14px) as primary surfaces
- Inter for UI; JetBrains Mono for paths, scores, and agent output
- Uppercase micro-labels (10–11px, wide tracking) for section and status chrome
- Semantic tints at low opacity (primary/15–20%, border at /20–30%) for actions and states
- Sparkle via inset glow (`--color-primary-glow`), hover light streaks, and `.live-indicator` on live status only
- Implementation classes in `web/src/index.css`: `focus-ring`, `briefing-card`, `session-row-selected`, `glow-primary`, `glow-accent`

## Colors

A restrained dual-accent palette: violet for primary orchestration, teal for secondary pipelines (Blog). Neutrals are tinted cool gray, never pure black or white.

### Primary

- **Violet Signal** (oklch(0.55 0.18 285) light / oklch(0.7 0.15 285) dark): Primary actions, active nav tab, DevLoop accents, live running states, MAB score chips. Used as tint + border, rarely as solid fill.
- **Violet Glow** (oklch(0.55 0.18 285 / 0.15) light / oklch(0.7 0.15 285 / 0.25) dark): Inset shadow on active tabs and focused primary buttons. The "sparkle" token.

### Secondary

- **Teal Beacon** (oklch(0.65 0.12 160)): Blog Pipeline section headers, create actions, selected blog cards. Same tint-not-fill discipline as violet.

### Tertiary

- **Semantic Moss / Amber / Ember** (success oklch(0.6 0.15 150), warning oklch(0.65 0.15 50), danger oklch(0.55 0.18 25)): Status dots, Start/Pause/Kill buttons, retry banners. Always paired with text labels; never hue-only signaling.

### Neutral

- **Frost Surface** (oklch(0.98 0.002 260)): Light mode page background with subtle radial violet/teal ambience.
- **Obsidian Surface** (oklch(0.08 0.01 260)): Dark mode page background.
- **Glass Frost / Glass Obsidian** (oklch(0.99 0.002 260 / 0.7) / oklch(0.16 0.01 260 / 0.65)): Primary `.glass` panels; backdrop blur 40px, saturate 180%.
- **Text Primary / Muted / Dim** (oklch(0.2 0.01 260) / oklch(0.45 0.01 260) / oklch(0.65 0.01 260)): Body, secondary copy, tertiary metadata.
- **Border Soft** (oklch(0 0 0 / 0.1) light, oklch(1 0 0 / 0.1) dark): Inputs, dividers, chip outlines.

### Named Rules

**The Tint-Not-Fill Rule.** Primary and accent colors appear at 10–30% opacity on backgrounds and borders. Solid saturated fills are forbidden on large surfaces.

**The One Sparkle Rule.** Glow, light streaks, and pulse animation apply only to live or selected states (running task, expanded orchestration, active nav). Static screens stay calm.

## Typography

**Display Font:** Inter (with system sans fallbacks)
**Body Font:** Inter (with system sans fallbacks)
**Label/Mono Font:** JetBrains Mono for data, paths, and agent identifiers

**Character:** Modern, tight-tracked UI sans with occasional heavy (900) display weight for briefing headlines. Uppercase micro-labels create instrument-panel rhythm without decorative display type in controls.

### Hierarchy

- **Display** (900, 22px, line-height 1.2): Briefing item titles, hero empty-state lines. Tight negative tracking (-0.035em).
- **Headline** (700, 18px, line-height 1.2): Section intros, empty-state primary message.
- **Title** (500, 14px, line-height 1.4): Card titles, orchestration names, form section labels at default case.
- **Body** (400, 15px, line-height 1.6): Descriptions, artifact preview, paragraph content. Cap prose at 65–75ch where readable.
- **Label** (700, 11px, letter-spacing 0.05–0.25em, uppercase): Tab nav, section headers ("Briefing Stream", "Development Loop"), status badges, timestamps.
- **Mono** (400, 13px): Project paths, git branches, MAB scores, task IDs, log excerpts.

### Named Rules

**The Instrument Label Rule.** Chrome labels (nav, section headers, metadata) use 10–11px uppercase with wide tracking. Never use display weight on buttons or form labels.

## Elevation

Hybrid system: tonal layering through glass translucency plus very soft ambient shadows. Not flat (glass + blur convey depth); not neumorphic (no inset extrusion on controls). **Borders stay constant on hover**; feedback uses background tint only.

Light mode `.glass` uses `var(--shadow-glass)` at rest. No hover shadow or border escalation on glass panels. Dark mode uses `var(--shadow-glass-dark)`, similarly flat at rest.

Popovers (`.info-tip-popover`) are **opaque**, not glass: they must not show content bleeding through.

### Shadow Vocabulary

- **Glass ambient** (`var(--shadow-glass)`): Default resting state for `.glass` panels. Light, diffuse.
- **Glass ambient dark** (`var(--shadow-glass-dark)`): Dark mode resting glass shadow.
- **Primary inset glow** (`var(--shadow-glow-inset)`): Active nav tab, selected cards, primary CTA. Inset only, no outer halo.
- **Accent inset glow** (`var(--shadow-glow-accent-inset)`): Selected Blog pipeline cards.
- **Popover float** (`0 2px 8px …, 0 8px 24px …`): Opaque tooltips and info tips.

### Named Rules

**The Flat Hover Rule.** Borders do not change color on hover. Hover feedback uses background luminosity (`--color-glass-emphasis`, `--color-glass-hover`, semantic tints). Selected states may use a **fixed** tinted border via `.glow-primary` / `.glow-accent`.

**The Opaque Popover Rule.** Overlays that carry instructions or errors use solid surfaces. Glass is for persistent layout panels, not floating help text.

## Components

Character: refined, restrained, tactile through border and glow rather than shadow extrusion.

### Buttons

- **Shape:** Soft corners (8px / `rounded-lg`); icon action buttons use 12px (`rounded-xl`, 36px square).
- **Primary:** Violet tint fill (primary/20), primary text, primary/30 border. Hover: primary/30 fill. Disabled: 40% opacity. Optional inset glow on submit actions.
- **Accent:** Teal tint pattern for Blog Pipeline CTAs (accent/15–20% fill, accent/20–30% border).
- **Small semantic:** 12px text, 1px border at `currentColor/20`, semantic text color (success, warning, danger). Hover: `currentColor/10` background. Used for Start, Pause, Kill, Edit.
- **Ghost / theme:** Transparent or glass-hover background; theme toggle uses 28px square, 6px radius.

### Chips

- **Style:** Uppercase mono or sans labels in pill containers (`rounded-full`, 10px text, border-border/50, bg-glass-subtle). Example: unread count badge.
- **State:** MAB score chip uses primary/10 bg, primary/20 border, mono numerals. Stage stepper chips: active stage gets primary/15 bg; complete/failed get semantic tints.

### Cards / Containers

- **Corner Style:** 20px (`.glass` main panels), 14px (`.glass-subtle` list rows and nested cards).
- **Background:** Frost/obsidian glass with 40px blur (main) or 16px blur (subtle).
- **Shadow Strategy:** Ambient glass shadow at rest; hover deepens shadow and may shift border toward primary in dark mode.
- **Border:** 1px glass-border token; hover brightens slightly.
- **Internal Padding:** 28px (`1.75rem`) on primary briefing cards; 16px on forms and pipeline cards.

### Inputs / Fields

- **Style:** `surface-alt` background, 1px border-border, 8px radius, 15px body text. Monospace for paths and multi-line agent context.
- **Focus:** Border shifts to primary; no default browser outline removal without `:focus-visible` ring (accessibility gap to close in implementation).
- **Error / Disabled:** Semantic banner pattern: warning/10 or danger/10 background with matching /20–25 border and label text.

### Navigation

- **Style:** Sticky glass header, max-width 6xl, centered with horizontal margins. Logo: 13px black uppercase "glimpse" with 6px primary dot glow.
- **Tabs:** 11px bold uppercase; active tab gets primary/10 bg + inset primary glow; inactive muted with glass-hover on hover.
- **Theme toggle:** Segmented glass-subtle control (light / system / dark) in header trailing edge.

### Signature Components

- **Briefing stream card:** Glass panel with hover light streak (gradient translate animation, 1000ms). Actions reveal on hover (opacity + translate). Respect `prefers-reduced-motion`: disable streak and scale.
- **Pipeline orchestration card:** Glass-subtle expandable row; expanded state uses `.glow-primary`. Stage stepper with arrow separators.
- **Blog pipeline card:** Selected state uses `.glow-accent` (teal sparkle, same discipline as primary).
- **Status dot:** 8px circle; running = primary + pulse; semantic colors for pending/complete/failed.
- **Logs split pane:** Fixed-width (320px) glass session list + flex detail pane at `calc(100vh - 8rem)`. Session rows select with glass-subtle highlight; running sessions poll at 3–5s.
- **Artifact preview:** Opaque `surface-alt/80` panel (not glass), 8px radius, mono filename header, copy control, `max-h-36` scrollable pre-wrap body. Iteration tabs as compact chips above preview.

## Do's and Don'ts

### Do:

- **Do** use OKLCH tokens from `@theme` in `web/src/index.css` as the single source of truth for color.
- **Do** pair semantic colors with text labels, icons, or dot + label combinations (Start, Pause, Kill, status dots with status text).
- **Do** keep accent coverage under ~10% of any screen; rarity preserves calm.
- **Do** use glass for persistent panels and opaque surfaces for tooltips/popovers.
- **Do** apply sparkle (glow, streak, `.live-indicator`) only on live, selected, or active elements.
- **Do** put `focus-ring` on every interactive control (buttons, inputs, selects, nav tabs).
- **Do** honor `prefers-reduced-motion` by disabling decorative hover streaks, card scale, and non-essential pulse.

### Don't:

- **Don't** use generic SaaS dashboards (identical card grids, hero metrics, purple-gradient CTAs).
- **Don't** use "AI slop" aesthetics (neon accents, high saturation, gradient text, decorative glass everywhere).
- **Don't** use chat-app UI (message bubbles as the primary metaphor).
- **Don't** build overly flat interfaces with no depth or hierarchy.
- **Don't** use neumorphism (soft extruded buttons, heavy inset shadows, skeuomorphic relief).
- **Don't** use high-saturation palettes or loud status colors that compete for attention.
- **Don't** bury primary actions under cluttered admin chrome.
- **Don't** use `border-left` or `border-right` greater than 1px as a colored stripe on cards or alerts.
- **Don't** use gradient text (`background-clip: text` with gradients).
- **Don't** rely on hue alone for status; color-blind users must read state from copy or iconography.
- **Don't** animate layout properties (width, height, margin); use opacity and transform only.
