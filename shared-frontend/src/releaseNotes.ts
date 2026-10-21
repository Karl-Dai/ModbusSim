export const RELEASE_HIGHLIGHTS = {
  'zh-CN': [
    '主从站全面采用 Go 协议核心和 Wails 桌面框架。',
    '保留线圈置 ON / 置 OFF、从站定向写入和停止轮询后的回读。',
    '本版请手动安装；导入旧工程后保存为 Go v2 格式，请保留原文件备份。'
  ],
  'en-US': [
    'Both apps now use the Go protocol core and Wails desktop host.',
    'Keep explicit coil controls, station-aware writes and readback with polling stopped.',
    'Install manually. Imported projects save in Go v2 format; keep the original backup.'
  ]
} as const
