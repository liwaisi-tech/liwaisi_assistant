import { useCallback, useMemo, useRef } from 'react';
import type { KeyboardEvent } from 'react';
import type { TFunction } from 'i18next';
import {
  REGIONAL_VARIANTS,
  type RegionalVariant,
} from '../../../types/setup';

interface RegionStepProps {
  t: TFunction;
  language: string;
  selectedVariant: RegionalVariant;
  onSelect: (variant: RegionalVariant) => void;
}

const FLAGS: Record<RegionalVariant, string> = {
  'es-CO': '🇨🇴',
  'es-MX': '🇲🇽',
  'es-AR': '🇦🇷',
  'es-ES': '🇪🇸',
  'en-GB': '🇬🇧',
  'en-US': '🇺🇸',
  'en-AU': '🇦🇺',
};

export function RegionStep({
  t,
  language,
  selectedVariant,
  onSelect,
}: RegionStepProps) {
  const baseLang = (language || 'es').split('-')[0] === 'en' ? 'en' : 'es';
  const group = REGIONAL_VARIANTS[baseLang];
  const defaultVariant = group.defaultVariant;

  const options = useMemo(() => group.variants, [group]);
  const buttonRefs = useRef<Array<HTMLButtonElement | null>>([]);

  const handleSelect = useCallback(
    (variant: RegionalVariant) => {
      onSelect(variant);
    },
    [onSelect],
  );

  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLButtonElement>, index: number) => {
      const cols = 2;
      let next = index;
      switch (e.key) {
        case 'ArrowRight':
          next = (index + 1) % options.length;
          break;
        case 'ArrowLeft':
          next = (index - 1 + options.length) % options.length;
          break;
        case 'ArrowDown':
          next = Math.min(index + cols, options.length - 1);
          break;
        case 'ArrowUp':
          next = Math.max(index - cols, 0);
          break;
        case 'Enter':
        case ' ':
          e.preventDefault();
          handleSelect(options[index].code);
          return;
        default:
          return;
      }
      e.preventDefault();
      buttonRefs.current[next]?.focus();
    },
    [options, handleSelect],
  );

  return (
    <div
      className="flex flex-col gap-6 py-4"
      style={{ animation: 'welcome-fade 0.4s ease-out forwards' }}
    >
      <style>{`
        @keyframes flag-wave {
          0%, 100% { transform: rotate(0deg) scale(1); }
          25%      { transform: rotate(-8deg) scale(1.12); }
          50%      { transform: rotate(6deg)  scale(1.18); }
          75%      { transform: rotate(-4deg) scale(1.12); }
        }
        @keyframes flag-pulse {
          0%, 100% { transform: scale(1); }
          50%      { transform: scale(1.06); }
        }
        .region-tile {
          will-change: transform, box-shadow;
          transition:
            transform 220ms cubic-bezier(0.34, 1.56, 0.64, 1),
            box-shadow 220ms ease,
            border-color 220ms ease,
            background-color 220ms ease;
        }
        .region-tile:hover,
        .region-tile:focus-visible {
          transform: translateY(-4px) scale(1.03);
        }
        .region-tile:active {
          transform: translateY(-1px) scale(0.99);
          transition-duration: 80ms;
        }
        .region-flag {
          display: inline-block;
          transform-origin: 50% 70%;
          transition: transform 220ms ease;
          filter: drop-shadow(0 4px 10px rgba(0, 0, 0, 0.35));
        }
        .region-tile:hover .region-flag,
        .region-tile:focus-visible .region-flag {
          animation: flag-wave 700ms ease-in-out;
        }
        .region-tile[data-selected="true"] .region-flag {
          animation: flag-pulse 2.4s ease-in-out infinite;
        }
      `}</style>

      <div className="text-center flex flex-col gap-2">
        <h2
          className="text-xl font-semibold tracking-tight"
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            color: 'var(--text-primary)',
          }}
        >
          {t('region.title')}
        </h2>
        <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
          {t('region.description')}
        </p>
      </div>

      <div
        role="radiogroup"
        aria-label={t('region.title')}
        className="grid grid-cols-2 gap-3"
      >
        {options.map((opt, i) => {
          const isSelected = selectedVariant === opt.code;
          const isDefault = defaultVariant === opt.code;
          return (
            <button
              key={opt.code}
              ref={(el) => {
                buttonRefs.current[i] = el;
              }}
              type="button"
              role="radio"
              aria-checked={isSelected}
              data-selected={isSelected}
              onClick={() => handleSelect(opt.code)}
              onKeyDown={(e) => handleKeyDown(e, i)}
              className="region-tile relative flex flex-col items-center justify-center gap-2 p-5 rounded-xl border cursor-pointer text-center outline-none"
              style={{
                backgroundColor: isSelected
                  ? 'rgba(14, 165, 233, 0.08)'
                  : 'var(--bg-input)',
                borderColor: isSelected ? 'var(--accent)' : 'var(--border-dim)',
                boxShadow: isSelected
                  ? '0 0 28px -6px var(--accent-glow), inset 0 0 0 1px var(--accent)'
                  : '0 1px 0 rgba(255,255,255,0.02)',
              }}
            >
              {isDefault && (
                <span
                  aria-hidden="true"
                  className="absolute top-1.5 right-1.5 text-[8px] px-1.5 py-0.5 rounded uppercase tracking-wider"
                  style={{
                    fontFamily: "'JetBrains Mono', monospace",
                    color: isSelected ? 'var(--accent)' : 'var(--text-muted)',
                    border: `1px solid ${
                      isSelected ? 'var(--accent)' : 'var(--border-dim)'
                    }`,
                    backgroundColor: 'var(--bg-input)',
                  }}
                >
                  {t('region.default_badge')}
                </span>
              )}

              <span
                className="region-flag text-5xl leading-none select-none"
                aria-hidden="true"
              >
                {FLAGS[opt.code]}
              </span>

              <span
                className="text-sm font-medium mt-1"
                style={{ color: 'var(--text-primary)' }}
              >
                {t(`region.variants.${opt.code}.label`)}
              </span>

              <span
                className="text-[10px] font-bold tracking-[0.15em] uppercase"
                style={{
                  fontFamily: "'JetBrains Mono', monospace",
                  color: isSelected ? 'var(--accent)' : 'var(--text-muted)',
                }}
              >
                {opt.code}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}
