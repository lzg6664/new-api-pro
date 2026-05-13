import { build } from 'vite';
import { createViteConfig } from '../vite.shared.js';

const config = await createViteConfig({
  command: 'build',
  mode: 'production',
});

await build({
  configFile: false,
  ...config,
});
