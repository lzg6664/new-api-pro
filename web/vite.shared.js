import react from '@vitejs/plugin-react';
import { transformWithEsbuild } from 'vite';
import pkg from '@douyinfe/vite-plugin-semi';
import path from 'path';
import { fileURLToPath } from 'url';

const { vitePluginSemi } = pkg;
const __dirname = path.dirname(fileURLToPath(import.meta.url));

export async function createViteConfig({ command }) {
  let inspectorPlugin = null;
  if (command === 'serve') {
    const { codeInspectorPlugin } = await import('code-inspector-plugin');
    inspectorPlugin = codeInspectorPlugin({
      bundler: 'vite',
    });
  }

  return {
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
      },
    },
    plugins: [
      // The inspector is only useful during local development and can
      // probe parent directories that are inaccessible in constrained builds.
      inspectorPlugin,
      {
        name: 'treat-js-files-as-jsx',
        async transform(code, id) {
          if (!/src\/.*\.js$/.test(id)) {
            return null;
          }

          return transformWithEsbuild(code, id, {
            loader: 'jsx',
            jsx: 'automatic',
          });
        },
      },
      react(),
      vitePluginSemi({
        cssLayer: true,
      }),
    ].filter(Boolean),
    optimizeDeps: {
      force: true,
      esbuildOptions: {
        loader: {
          '.js': 'jsx',
          '.json': 'json',
        },
      },
    },
    build: {
      rollupOptions: {
        output: {
          manualChunks: {
            'react-core': ['react', 'react-dom', 'react-router-dom'],
            'semi-ui': ['@douyinfe/semi-icons', '@douyinfe/semi-ui'],
            tools: ['axios', 'history', 'marked'],
            'react-components': [
              'react-dropzone',
              'react-fireworks',
              'react-telegram-login',
              'react-toastify',
              'react-turnstile',
            ],
            i18n: [
              'i18next',
              'react-i18next',
              'i18next-browser-languagedetector',
            ],
          },
        },
      },
    },
    server: {
      host: '0.0.0.0',
      proxy: {
        '/api': {
          target: 'http://localhost:3000',
          changeOrigin: true,
        },
        '/mj': {
          target: 'http://localhost:3000',
          changeOrigin: true,
        },
        '/pg': {
          target: 'http://localhost:3000',
          changeOrigin: true,
        },
      },
    },
  };
}
