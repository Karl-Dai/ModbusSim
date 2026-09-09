import { save } from '@tauri-apps/plugin-dialog'
import { invoke } from '@tauri-apps/api/core'
export async function saveExport(content: string, defaultPath: string) {
  const path = await save({ defaultPath, filters: [{ name: 'CSV', extensions: ['csv'] }] })
  if (path) await invoke('save_text_export', { path, content })
}
