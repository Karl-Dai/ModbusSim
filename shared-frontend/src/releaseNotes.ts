export const RELEASE_HIGHLIGHTS = {
  "zh-CN": [
    "主站线圈增加置 ON / 置 OFF 按钮，明确显示下发进度和失败原因。",
    "写入后回读同一扫描组和从站，停止轮询时仍同步表格与解析值。",
    "连接、扫描组和数据行支持键盘选择，回读失败明确标识旧值。"
  ],
  "en-US": [
    "Explicit Set ON / Set OFF coil controls show sending status and failure details.",
    "Writes read back the same scan group and slave, updating values even with polling stopped.",
    "Connections, scan groups and data rows support keyboard selection, with clear stale-value readback errors."
  ]
} as const
