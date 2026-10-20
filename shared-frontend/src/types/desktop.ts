import type { InjectionKey } from 'vue'
/** Host features used by shared UI; legacy master keeps its Tauri host. */
export interface DesktopServices {
  getVersion: () => Promise<string>
  openURL: (url: string) => void
}
export const desktopServicesKey: InjectionKey<DesktopServices> = Symbol('desktopServices')
