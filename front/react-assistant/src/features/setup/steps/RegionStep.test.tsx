import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { I18nextProvider } from 'react-i18next';
import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import setupEn from '../../../i18n/locales/en/setup.json';
import setupEs from '../../../i18n/locales/es/setup.json';
import { RegionStep } from './RegionStep';
import { defaultVariantForLanguage } from '../../../types/setup';

const testI18n = i18n.createInstance();
testI18n.use(initReactI18next).init({
  lng: 'en',
  fallbackLng: 'en',
  resources: {
    en: { setup: setupEn },
    es: { setup: setupEs },
  },
  defaultNS: 'setup',
  interpolation: { escapeValue: false },
});

function renderStep(language: 'es' | 'en', onSelect = vi.fn()) {
  const t = testI18n.getFixedT(null, 'setup');
  const selected = defaultVariantForLanguage(language);
  const utils = render(
    <I18nextProvider i18n={testI18n}>
      <RegionStep
        t={t}
        language={language}
        selectedVariant={selected}
        onSelect={onSelect}
      />
    </I18nextProvider>,
  );
  return { ...utils, onSelect };
}

describe('RegionStep', () => {
  it('renders all four es-* variants and selects es-CO by default', () => {
    renderStep('es');
    expect(screen.getByText('es-CO')).toBeInTheDocument();
    expect(screen.getByText('es-MX')).toBeInTheDocument();
    expect(screen.getByText('es-AR')).toBeInTheDocument();
    expect(screen.getByText('es-ES')).toBeInTheDocument();

    const radios = screen.getAllByRole('radio');
    const checked = radios.find((r) => r.getAttribute('aria-checked') === 'true');
    expect(checked).toBeTruthy();
    expect(checked?.textContent).toContain('es-CO');
  });

  it('renders all three en-* variants and selects en-GB by default', () => {
    renderStep('en');
    expect(screen.getByText('en-GB')).toBeInTheDocument();
    expect(screen.getByText('en-US')).toBeInTheDocument();
    expect(screen.getByText('en-AU')).toBeInTheDocument();
    expect(screen.queryByText('es-CO')).not.toBeInTheDocument();

    const radios = screen.getAllByRole('radio');
    const checked = radios.find((r) => r.getAttribute('aria-checked') === 'true');
    expect(checked?.textContent).toContain('en-GB');
  });

  it('calls onSelect with the picked variant on click', () => {
    const { onSelect } = renderStep('es');
    const mxButton = screen
      .getAllByRole('radio')
      .find((r) => r.textContent?.includes('es-MX'));
    expect(mxButton).toBeTruthy();
    fireEvent.click(mxButton!);
    expect(onSelect).toHaveBeenCalledWith('es-MX');
  });
});
