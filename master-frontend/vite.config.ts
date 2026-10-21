import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: { outDir: '../go/cmd/mastergui/assets/dist', emptyOutDir: true },
  server: {
    port: 5174,
    strictPort: true,
  },
})
