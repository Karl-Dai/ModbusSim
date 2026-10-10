export const RELEASE_HIGHLIGHTS = {
  "zh-CN": [
    "主站浅色主题统一为白色数据区、浅灰侧栏和清晰的深色文字，连接树、值解析与弹窗不再混用深色样式。",
    "寄存器和线圈写入遵循扫描组指定的从站 ID，并显示实际写入目标；未指定时继续使用连接默认 ID。",
    "多字解析和写入要求同类型、连续地址的寄存器选择，避免将不连续地址误写为连续范围。"
  ],
  "en-US": [
    "Master light mode now uses white data surfaces, light gray sidebars and readable dark text consistently across the connection tree, value panel and dialogs.",
    "Register and coil writes honor the scan group's unit ID and show the write target, while retaining the connection default when no override is set.",
    "Multiword interpretation and writes require same-type, contiguous register selections, preventing sparse selections from being written as a packed range."
  ]
} as const
