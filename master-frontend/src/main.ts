import { GetVersion } from '../wailsjs/go/main/App'
import { BrowserOpenURL } from '../wailsjs/runtime/runtime'
import 'shared-frontend/src/styles/tokens.css'
import 'shared-frontend/src/styles/toolbar.css'
import { desktopServicesKey, initTheme } from 'shared-frontend'
import { createApp } from 'vue'
import App from './App.vue'

initTheme()
createApp(App).provide(desktopServicesKey, { getVersion: GetVersion, openURL: BrowserOpenURL }).mount('#app')
