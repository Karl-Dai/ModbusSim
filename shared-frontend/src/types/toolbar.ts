export interface ToolbarAction {
  id: string
  label: string
  title?: string
  disabled?: boolean
  busy?: boolean
  shortcut?: string
  separatorBefore?: boolean
  tone?: 'start' | 'stop' | 'danger'
  icon?: 'add' | 'save' | 'play' | 'stop' | 'scan' | 'write' | 'device'
  action: () => unknown
}

export interface ToolbarMenuDefinition {
  id: string
  label: string
  items: ToolbarAction[]
}
