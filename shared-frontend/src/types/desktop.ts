import type { InjectionKey } from 'vue'
/** Go/Wails host features used by shared UI. */
export interface DesktopServices {
  getVersion: () => Promise<string>
  openURL: (url: string) => void
}
export const desktopServicesKey: InjectionKey<DesktopServices> = Symbol('desktopServices')
