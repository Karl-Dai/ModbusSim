export const RELEASE_HIGHLIGHTS = {
  "zh-CN": [
    "证书路径字段优先显示文件名，悬停查看完整路径，点击即可编辑。",
    "macOS 文件选择框改为可自由拖动的独立窗口。",
    "子站支持 PEM 格式的 RSA 私钥，TLS 启动失败会明确报错。"
  ],
  "en-US": [
    "Certificate path fields lead with the file name; hover for the full path, click to edit.",
    "The macOS file picker is now a free-standing window that can be dragged anywhere.",
    "Slave accepts PEM-encoded RSA keys and reports TLS startup failures clearly."
  ]
} as const
