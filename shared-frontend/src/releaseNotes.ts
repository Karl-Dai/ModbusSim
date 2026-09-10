export const RELEASE_HIGHLIGHTS = {
  "zh-CN": [
    "修复 macOS 加载 PEM 客户端证书时重复导入私钥、提示钥匙串条目已存在的问题。",
    "新增明确的旧设备证书兼容选项，支持缺少 EKU/SAN 的测试设备；默认保留严格验证。",
    "TLS 连接支持从站 ID 和寄存器扫描，保留分块、请求间隔、进度、取消及原从站 ID。",
    "主站与从站共用菜单栏和快捷操作；主站可编辑已有连接设置而保留扫描组。"
  ],
  "en-US": [
    "Fix duplicate private-key imports when loading PEM client certificates on macOS.",
    "Provide explicit legacy-certificate compatibility for test devices without EKU/SAN while retaining strict verification by default.",
    "Support slave-ID and register scans over TLS with chunking, pacing, progress, cancellation and preserved unit IDs.",
    "Share menus and quick actions between both apps, and edit existing master connection settings without losing scan groups."
  ]
} as const
