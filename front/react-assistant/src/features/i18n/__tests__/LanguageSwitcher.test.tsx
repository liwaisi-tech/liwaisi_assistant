import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, afterEach } from 'vitest';
import { I18nTestWrapper, testI18n } from '../../../test/i18n-test-utils';
import { LanguageSwitcher } from '../LanguageSwitcher';

const wrapper = I18nTestWrapper;

describe('LanguageSwitcher', () => {
  afterEach(() => {
    testI18n.changeLanguage('en');
  });

  it('should render with current language code', () => {
    render(<LanguageSwitcher variant="landing" />, { wrapper });

    expect(screen.getByText('EN')).toBeInTheDocument();
  });

  it('should open dropdown on click', () => {
    render(<LanguageSwitcher variant="landing" />, { wrapper });

    fireEvent.click(screen.getByRole('button'));

    expect(screen.getByRole('listbox')).toBeInTheDocument();
    expect(screen.getByText('English')).toBeInTheDocument();
    expect(screen.getByText('Español')).toBeInTheDocument();
  });

  it('should change language on selection', () => {
    render(<LanguageSwitcher variant="landing" />, { wrapper });

    fireEvent.click(screen.getByRole('button'));
    fireEvent.click(screen.getByText('Español'));

    expect(testI18n.language).toBe('es');
    expect(screen.getByText('ES')).toBeInTheDocument();
  });

  it('should close dropdown on Escape', () => {
    render(<LanguageSwitcher variant="landing" />, { wrapper });

    fireEvent.click(screen.getByRole('button'));
    expect(screen.getByRole('listbox')).toBeInTheDocument();

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
  });

  it('should have correct aria-expanded attribute', () => {
    render(<LanguageSwitcher variant="landing" />, { wrapper });

    const button = screen.getByRole('button');
    expect(button).toHaveAttribute('aria-expanded', 'false');

    fireEvent.click(button);
    expect(button).toHaveAttribute('aria-expanded', 'true');
  });

  it('should have aria-haspopup attribute', () => {
    render(<LanguageSwitcher variant="landing" />, { wrapper });
    expect(screen.getByRole('button')).toHaveAttribute('aria-haspopup', 'listbox');
  });

  it('should mark active language with aria-selected', () => {
    render(<LanguageSwitcher variant="landing" />, { wrapper });

    fireEvent.click(screen.getByRole('button'));

    const options = screen.getAllByRole('option');
    const enOption = options.find(o => o.textContent?.includes('English'));
    const esOption = options.find(o => o.textContent?.includes('Español'));

    expect(enOption).toHaveAttribute('aria-selected', 'true');
    expect(esOption).toHaveAttribute('aria-selected', 'false');
  });

  it('should announce language change via aria-live', () => {
    render(<LanguageSwitcher variant="landing" />, { wrapper });

    fireEvent.click(screen.getByRole('button'));
    fireEvent.click(screen.getByText('Español'));

    const liveRegion = document.querySelector('[aria-live="polite"]');
    expect(liveRegion?.textContent).toContain('Español');
  });

  it('should render desktop variant', () => {
    render(<LanguageSwitcher variant="desktop" />, { wrapper });

    expect(screen.getByText('EN')).toBeInTheDocument();
    expect(screen.getByRole('button')).toBeInTheDocument();
  });

  it('should render landing variant', () => {
    render(<LanguageSwitcher variant="landing" />, { wrapper });

    expect(screen.getByText('EN')).toBeInTheDocument();
  });
});
