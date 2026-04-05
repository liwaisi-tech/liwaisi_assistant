import { useTranslation } from 'react-i18next';

export function StreamingIndicator() {
  const { t } = useTranslation('chat');

  return (
    <div className="flex justify-start">
      <div className="flex items-center gap-2 px-4 py-3 rounded-2xl rounded-bl-md"
           style={{ backgroundColor: 'var(--bg-assistant)', border: '1px solid var(--border-dim)' }}>
        <div className="flex items-center gap-1.5" role="status" aria-label={t('streaming.thinking')}>
          <span className="thinking-dot" />
          <span className="thinking-dot" />
          <span className="thinking-dot" />
        </div>
      </div>
    </div>
  );
}
