import 'shared-frontend/src/styles/tokens.css'
import { createApp } from 'vue'
import './style.css'
import 'shared-frontend/src/styles/transitions.css'
import 'shared-frontend/src/styles/toolbar.css'
import { desktopServicesKey, initTheme } from 'shared-frontend'
import App from './App.vue'

import { GetVersion } from '../wailsjs/go/main/App'
import { BrowserOpenURL } from '../wailsjs/runtime/runtime'

initTheme()
createApp(App).provide(desktopServicesKey, { getVersion: GetVersion, openURL: BrowserOpenURL }).mount('#app')
