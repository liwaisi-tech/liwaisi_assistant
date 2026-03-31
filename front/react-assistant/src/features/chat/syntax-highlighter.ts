import PrismLight from 'react-syntax-highlighter/dist/esm/prism-light';
import tsx from 'react-syntax-highlighter/dist/esm/languages/prism/tsx';
import typescript from 'react-syntax-highlighter/dist/esm/languages/prism/typescript';
import javascript from 'react-syntax-highlighter/dist/esm/languages/prism/javascript';
import python from 'react-syntax-highlighter/dist/esm/languages/prism/python';
import go from 'react-syntax-highlighter/dist/esm/languages/prism/go';
import bash from 'react-syntax-highlighter/dist/esm/languages/prism/bash';
import json from 'react-syntax-highlighter/dist/esm/languages/prism/json';
import yaml from 'react-syntax-highlighter/dist/esm/languages/prism/yaml';
import sql from 'react-syntax-highlighter/dist/esm/languages/prism/sql';
import css from 'react-syntax-highlighter/dist/esm/languages/prism/css';
import markup from 'react-syntax-highlighter/dist/esm/languages/prism/markup';

PrismLight.registerLanguage('tsx', tsx);
PrismLight.registerLanguage('typescript', typescript);
PrismLight.registerLanguage('ts', typescript);
PrismLight.registerLanguage('javascript', javascript);
PrismLight.registerLanguage('js', javascript);
PrismLight.registerLanguage('python', python);
PrismLight.registerLanguage('py', python);
PrismLight.registerLanguage('go', go);
PrismLight.registerLanguage('bash', bash);
PrismLight.registerLanguage('sh', bash);
PrismLight.registerLanguage('shell', bash);
PrismLight.registerLanguage('json', json);
PrismLight.registerLanguage('yaml', yaml);
PrismLight.registerLanguage('yml', yaml);
PrismLight.registerLanguage('sql', sql);
PrismLight.registerLanguage('css', css);
PrismLight.registerLanguage('html', markup);
PrismLight.registerLanguage('xml', markup);

/**
 * Custom Prism theme — deep-space terminal palette.
 * Uses only colors from the app's CSS custom properties
 * to maintain visual coherence with the rest of the UI.
 */
export const deepSpaceTheme: Record<string, React.CSSProperties> = {
  'code[class*="language-"]': {
    color: '#e2e8f0',
    fontFamily: "'JetBrains Mono', monospace",
    fontSize: '0.8125rem',
    lineHeight: '1.6',
    whiteSpace: 'pre',
    wordSpacing: 'normal',
    wordBreak: 'normal',
    tabSize: 2,
    hyphens: 'none',
  },
  'pre[class*="language-"]': {
    color: '#e2e8f0',
    fontFamily: "'JetBrains Mono', monospace",
    fontSize: '0.8125rem',
    lineHeight: '1.6',
    whiteSpace: 'pre',
    wordSpacing: 'normal',
    wordBreak: 'normal',
    tabSize: 2,
    hyphens: 'none',
    margin: 0,
    padding: 0,
    background: 'transparent',
    overflow: 'auto',
  },
  'comment': { color: '#64748b', fontStyle: 'italic' },
  'prolog': { color: '#64748b' },
  'doctype': { color: '#64748b' },
  'cdata': { color: '#64748b' },
  'punctuation': { color: '#94a3b8' },
  'namespace': { opacity: 0.7 },
  'property': { color: '#7dd3fc' },
  'tag': { color: '#0ea5e9' },
  'boolean': { color: '#7dd3fc' },
  'number': { color: '#7dd3fc' },
  'constant': { color: '#7dd3fc' },
  'symbol': { color: '#7dd3fc' },
  'deleted': { color: '#f87171' },
  'selector': { color: '#38bdf8' },
  'attr-name': { color: '#38bdf8' },
  'string': { color: '#38bdf8' },
  'char': { color: '#38bdf8' },
  'builtin': { color: '#38bdf8' },
  'inserted': { color: '#4ade80' },
  'operator': { color: '#94a3b8' },
  'entity': { color: '#94a3b8', cursor: 'help' },
  'url': { color: '#94a3b8' },
  'atrule': { color: '#0ea5e9' },
  'attr-value': { color: '#38bdf8' },
  'keyword': { color: '#0ea5e9' },
  'function': { color: '#e2e8f0' },
  'class-name': { color: '#cbd5e1' },
  'regex': { color: '#38bdf8' },
  'important': { color: '#0ea5e9', fontWeight: 'bold' },
  'variable': { color: '#e2e8f0' },
};

export default PrismLight;
