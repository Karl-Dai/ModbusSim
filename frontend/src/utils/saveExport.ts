import * as App from '../../wailsjs/go/main/App'
export async function saveExport(content: string, defaultPath: string) {
  const path = await App.SaveFileDialog(defaultPath, [{ Name: 'CSV', Extensions: ['csv'] }])
  if (path) await App.SaveTextExport(path, content)
}
