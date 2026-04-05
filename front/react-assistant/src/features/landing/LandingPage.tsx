import { useState, useEffect, useCallback, useRef } from 'react';
import { HeroSection } from './HeroSection';
import { WhatIsSection } from './WhatIsSection';
import { HowItWorksSection } from './HowItWorksSection';
import { FeaturesSection } from './FeaturesSection';
import { MissionSection } from './MissionSection';
import { FooterCTA } from './FooterCTA';
import { SignInModal } from './SignInModal';

const NAV_LINKS = [
  { href: '#what-is', label: 'About' },
  { href: '#how-it-works', label: 'How It Works' },
  { href: '#features', label: 'Features' },
  { href: '#mission', label: 'Our Mission' },
  { href: '#get-access', label: 'Get Access' },
];

function LandingNav({ onSignIn }: { onSignIn: () => void }) {
  const [scrolled, setScrolled] = useState(false);
  const [active, setActive] = useState('');
  const [dropdownOpen, setDropdownOpen] = useState(false);
  const googleBtnRef = useRef<HTMLDivElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handleScroll = () => {
      setScrolled(window.scrollY > 80);
    };

    const sectionIds = NAV_LINKS.map((l) => l.href.slice(1));
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) {
            setActive(entry.target.id);
          }
        }
      },
      { rootMargin: '-40% 0px -40% 0px' },
    );

    window.addEventListener('scroll', handleScroll, { passive: true });
    handleScroll();

    for (const id of sectionIds) {
      const el = document.getElementById(id);
      if (el) observer.observe(el);
    }

    return () => {
      window.removeEventListener('scroll', handleScroll);
      observer.disconnect();
    };
  }, []);

  // Render Google button when dropdown opens
  useEffect(() => {
    if (!dropdownOpen) return;

    const renderButton = () => {
      if (googleBtnRef.current && window.google?.accounts) {
        googleBtnRef.current.innerHTML = '';
        window.google.accounts.id.renderButton(googleBtnRef.current, {
          theme: 'filled_black',
          size: 'large',
          text: 'signin_with',
          width: 240,
        });
      }
    };

    if (window.google?.accounts) {
      // Small delay to ensure the DOM container is visible before Google renders
      requestAnimationFrame(renderButton);
    } else {
      const checkInterval = setInterval(() => {
        if (window.google?.accounts) {
          clearInterval(checkInterval);
          renderButton();
        }
      }, 100);
      return () => clearInterval(checkInterval);
    }
  }, [dropdownOpen]);

  // Close dropdown on outside click or Escape
  useEffect(() => {
    if (!dropdownOpen) return;

    const handleClick = (e: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setDropdownOpen(false);
      }
    };
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setDropdownOpen(false);
    };

    document.addEventListener('mousedown', handleClick);
    document.addEventListener('keydown', handleKey);
    return () => {
      document.removeEventListener('mousedown', handleClick);
      document.removeEventListener('keydown', handleKey);
    };
  }, [dropdownOpen]);

  return (
    <nav
      className="fixed top-0 left-0 right-0 z-40 transition-all duration-300"
      style={{
        backgroundColor: scrolled ? 'rgba(10, 10, 15, 0.88)' : 'transparent',
        backdropFilter: scrolled ? 'blur(12px)' : 'none',
        WebkitBackdropFilter: scrolled ? 'blur(12px)' : 'none',
        borderBottom: scrolled ? '1px solid var(--border-dim)' : '1px solid transparent',
      }}
      aria-label="Landing page navigation"
    >
      <div className="max-w-6xl mx-auto px-4 sm:px-8 h-14 flex items-center justify-between">
        <a href="#hero" className="shrink-0">
          <img src="/liwaisi_logo_dark_bg.svg" alt="Liwaisi" className="h-6 w-auto" />
        </a>

        <div className="flex items-center gap-6">
          {/* Section links — only visible when scrolled */}
          <div
            className="hidden sm:flex items-center gap-6 transition-opacity duration-300"
            style={{ opacity: scrolled ? 1 : 0, pointerEvents: scrolled ? 'auto' : 'none' }}
          >
            {NAV_LINKS.map((link) => (
              <a
                key={link.href}
                href={link.href}
                className="text-xs transition-colors"
                style={{
                  fontFamily: "'JetBrains Mono', monospace",
                  color: active === link.href.slice(1) ? 'var(--accent)' : 'var(--text-muted)',
                }}
                onMouseEnter={(e) => (e.currentTarget.style.color = 'var(--accent)')}
                onMouseLeave={(e) => {
                  e.currentTarget.style.color =
                    active === link.href.slice(1) ? 'var(--accent)' : 'var(--text-muted)';
                }}
              >
                {link.label}
              </a>
            ))}
          </div>

          {/* Sign In — always visible, with inline dropdown */}
          <div className="relative" ref={dropdownRef}>
            <button
              onClick={() => setDropdownOpen((p) => !p)}
              className="flex items-center gap-2 px-4 py-1.5 rounded-full text-xs font-medium transition-all duration-200 cursor-pointer"
              style={{
                fontFamily: "'JetBrains Mono', monospace",
                backgroundColor: dropdownOpen ? 'var(--bg-input)' : 'transparent',
                border: '1px solid',
                borderColor: dropdownOpen ? 'var(--accent)' : 'var(--border-dim)',
                color: dropdownOpen ? 'var(--accent)' : 'var(--text-secondary)',
                boxShadow: dropdownOpen ? '0 0 12px -2px var(--accent-glow)' : 'none',
              }}
              onMouseEnter={(e) => {
                if (!dropdownOpen) {
                  e.currentTarget.style.borderColor = 'var(--accent)';
                  e.currentTarget.style.color = 'var(--accent)';
                  e.currentTarget.style.boxShadow = '0 0 12px -2px var(--accent-glow)';
                }
              }}
              onMouseLeave={(e) => {
                if (!dropdownOpen) {
                  e.currentTarget.style.borderColor = 'var(--border-dim)';
                  e.currentTarget.style.color = 'var(--text-secondary)';
                  e.currentTarget.style.boxShadow = 'none';
                }
              }}
              aria-expanded={dropdownOpen}
              aria-haspopup="true"
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" className="shrink-0">
                <circle cx="12" cy="8" r="4" stroke="currentColor" strokeWidth="1.5" />
                <path d="M5 20c0-3.87 3.13-7 7-7s7 3.13 7 7" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              </svg>
              Sign In
            </button>

            {/* Inline dropdown with Google Sign-In */}
            {dropdownOpen && (
              <div
                className="signin-panel absolute right-0 top-full mt-2 w-72 rounded-xl border overflow-hidden"
                style={{
                  backgroundColor: 'rgba(18, 18, 26, 0.95)',
                  backdropFilter: 'blur(20px)',
                  WebkitBackdropFilter: 'blur(20px)',
                  borderColor: 'var(--border-dim)',
                  boxShadow: '0 0 0 1px var(--border-dim), 0 0 32px -8px var(--accent-glow), 0 16px 48px -16px rgba(0,0,0,0.7)',
                }}
              >
                {/* Top accent line */}
                <div
                  className="h-px w-full"
                  style={{ background: 'linear-gradient(90deg, transparent, var(--accent), transparent)' }}
                />

                <div className="p-5 flex flex-col items-center gap-4">
                  <div className="text-center">
                    <p
                      className="text-sm font-semibold mb-0.5"
                      style={{ fontFamily: "'JetBrains Mono', monospace", color: 'var(--text-primary)' }}
                    >
                      Welcome back
                    </p>
                    <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
                      Sign in to your command center
                    </p>
                  </div>

                  <div ref={googleBtnRef} className="flex justify-center" />

                  <div className="w-full flex items-center gap-2">
                    <div className="flex-1 h-px" style={{ backgroundColor: 'var(--border-dim)' }} />
                    <span className="text-[9px] uppercase tracking-widest" style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}>
                      or
                    </span>
                    <div className="flex-1 h-px" style={{ backgroundColor: 'var(--border-dim)' }} />
                  </div>

                  <button
                    onClick={() => {
                      setDropdownOpen(false);
                      onSignIn();
                    }}
                    className="w-full py-2 rounded-lg text-xs transition-colors cursor-pointer"
                    style={{
                      fontFamily: "'JetBrains Mono', monospace",
                      backgroundColor: 'var(--bg-input)',
                      border: '1px solid var(--border-dim)',
                      color: 'var(--text-secondary)',
                    }}
                    onMouseEnter={(e) => {
                      e.currentTarget.style.borderColor = 'var(--accent)';
                      e.currentTarget.style.color = 'var(--accent)';
                    }}
                    onMouseLeave={(e) => {
                      e.currentTarget.style.borderColor = 'var(--border-dim)';
                      e.currentTarget.style.color = 'var(--text-secondary)';
                    }}
                  >
                    Join Waitlist Instead
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
      </div>
    </nav>
  );
}

export function LandingPage() {
  const [showSignIn, setShowSignIn] = useState(false);

  const handleOpenSignIn = useCallback(() => setShowSignIn(true), []);
  const handleCloseSignIn = useCallback(() => setShowSignIn(false), []);

  return (
    <div className="landing-scroll" style={{ backgroundColor: 'var(--bg-deep)' }}>
      <LandingNav onSignIn={handleOpenSignIn} />
      <main>
        <HeroSection />
        <WhatIsSection />
        <HowItWorksSection />
        <FeaturesSection />
        <MissionSection />
        <FooterCTA onSignIn={handleOpenSignIn} />
      </main>
      <SignInModal open={showSignIn} onClose={handleCloseSignIn} />
    </div>
  );
}
