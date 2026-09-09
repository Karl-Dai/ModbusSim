export const RELEASE_HIGHLIGHTS = {
  "zh-CN": [
    "从站公共界面对齐 IEC104：统一分组菜单、可拖动布局、弹窗和中文寄存器分组。",
    "显示真实客户端数量、对端地址与连接时间，支持 TCP、TLS、RTU-over-TCP。",
    "补齐寄存器 CSV 导入导出、批量操作、报文解析和日志交互；变位启停归入模拟设置。",
    "主站支持请求间隔、单次读取上限和重连参数，读取自动分包并保留旧项目默认值。"
  ],
  "en-US": [
    "Slave common UI aligned with IEC104: grouped menus, resizable layout, consistent dialogs and localized register groups.",
    "Show live client counts, peer addresses and connection times for TCP, TLS and RTU-over-TCP.",
    "Add register CSV workflows, batch actions, frame tools and richer logs; control point mutation through simulation settings.",
    "Master adds request pacing, read limits and reconnect settings, with automatic read batching and backward-compatible project defaults."
  ]
} as const
