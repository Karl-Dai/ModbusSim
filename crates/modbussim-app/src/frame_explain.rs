use modbussim_core::{frame, tools};

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Lang {
    Zh,
    En,
}

impl Lang {
    pub fn from_code(code: &str) -> Self {
        if code.starts_with("zh") {
            Self::Zh
        } else {
            Self::En
        }
    }

    fn pick(self, zh: String, en: String) -> String {
        match self {
            Self::Zh => zh,
            Self::En => en,
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Kind {
    Bits,
    Regs,
    WriteSingleCoil,
    WriteSingleReg,
    WriteMultiCoil,
    WriteMultiReg,
}

struct FcInfo {
    zh: &'static str,
    en: &'static str,
    kind: Kind,
    plc_base: u32,
    unit_zh: &'static str,
    unit_en: &'static str,
    unit_en_plural: &'static str,
    max_qty: u16,
}

fn fc_info(fc: u8) -> Option<FcInfo> {
    Some(match fc {
        1 => FcInfo {
            zh: "读线圈",
            en: "Read Coils",
            kind: Kind::Bits,
            plc_base: 0,
            unit_zh: "线圈",
            unit_en: "coil",
            unit_en_plural: "coils",
            max_qty: 2000,
        },
        2 => FcInfo {
            zh: "读离散输入",
            en: "Read Discrete Inputs",
            kind: Kind::Bits,
            plc_base: 10000,
            unit_zh: "离散输入",
            unit_en: "discrete input",
            unit_en_plural: "discrete inputs",
            max_qty: 2000,
        },
        3 => FcInfo {
            zh: "读保持寄存器",
            en: "Read Holding Registers",
            kind: Kind::Regs,
            plc_base: 40000,
            unit_zh: "寄存器",
            unit_en: "register",
            unit_en_plural: "registers",
            max_qty: 125,
        },
        4 => FcInfo {
            zh: "读输入寄存器",
            en: "Read Input Registers",
            kind: Kind::Regs,
            plc_base: 30000,
            unit_zh: "寄存器",
            unit_en: "register",
            unit_en_plural: "registers",
            max_qty: 125,
        },
        5 => FcInfo {
            zh: "写单个线圈",
            en: "Write Single Coil",
            kind: Kind::WriteSingleCoil,
            plc_base: 0,
            unit_zh: "线圈",
            unit_en: "coil",
            unit_en_plural: "coils",
            max_qty: 0,
        },
        6 => FcInfo {
            zh: "写单个寄存器",
            en: "Write Single Register",
            kind: Kind::WriteSingleReg,
            plc_base: 40000,
            unit_zh: "寄存器",
            unit_en: "register",
            unit_en_plural: "registers",
            max_qty: 0,
        },
        15 => FcInfo {
            zh: "写多个线圈",
            en: "Write Multiple Coils",
            kind: Kind::WriteMultiCoil,
            plc_base: 0,
            unit_zh: "线圈",
            unit_en: "coil",
            unit_en_plural: "coils",
            max_qty: 1968,
        },
        16 => FcInfo {
            zh: "写多个寄存器",
            en: "Write Multiple Registers",
            kind: Kind::WriteMultiReg,
            plc_base: 40000,
            unit_zh: "寄存器",
            unit_en: "register",
            unit_en_plural: "registers",
            max_qty: 123,
        },
        _ => return None,
    })
}

fn exception_info(code: u8, lang: Lang) -> Option<(&'static str, &'static str)> {
    let zh = matches!(lang, Lang::Zh);
    Some(match code {
        0x01 => {
            if zh {
                ("非法功能", "从站不认识这个功能码，可能不支持该操作")
            } else {
                (
                    "Illegal function",
                    "the slave does not recognize this function code; the operation may not be supported",
                )
            }
        }
        0x02 => {
            if zh {
                ("非法数据地址", "要访问的寄存器在从站中不存在")
            } else {
                (
                    "Illegal data address",
                    "the register being accessed does not exist on the slave",
                )
            }
        }
        0x03 => {
            if zh {
                ("非法数据值", "请求里的数值超出了从站允许的范围")
            } else {
                (
                    "Illegal data value",
                    "the value in the request is outside the range the slave allows",
                )
            }
        }
        0x04 => {
            if zh {
                ("从站设备故障", "从站执行操作时发生内部错误")
            } else {
                (
                    "Slave device failure",
                    "an internal error occurred while the slave executed the operation",
                )
            }
        }
        0x05 => {
            if zh {
                ("确认", "从站已接受请求并正在处理，需要较长时间，可稍后再查")
            } else {
                (
                    "Acknowledge",
                    "the slave accepted the request and is processing it; this takes a while, check again later",
                )
            }
        }
        0x06 => {
            if zh {
                ("从站设备忙", "从站正在处理其他任务，请稍后重试")
            } else {
                (
                    "Slave device busy",
                    "the slave is handling other tasks; try again later",
                )
            }
        }
        0x08 => {
            if zh {
                ("存储奇偶性差错", "从站读取内部存储时校验失败")
            } else {
                (
                    "Memory parity error",
                    "the slave failed a parity check while reading its internal memory",
                )
            }
        }
        0x0A => {
            if zh {
                ("网关路径不可用", "网关无法把请求转发到目标设备")
            } else {
                (
                    "Gateway path unavailable",
                    "the gateway could not forward the request to the target device",
                )
            }
        }
        0x0B => {
            if zh {
                ("网关目标设备无响应", "网关后面的目标设备没有回应")
            } else {
                (
                    "Gateway target device failed to respond",
                    "the device behind the gateway did not respond",
                )
            }
        }
        _ => return None,
    })
}

struct Field {
    hex: String,
    label: String,
    desc: String,
}

impl Field {
    fn new(bytes: &[u8], label: String, desc: String) -> Self {
        Self {
            hex: tools::format_hex(bytes, " "),
            label,
            desc,
        }
    }
}

struct Body {
    summary: String,
    detail_rows: Vec<String>,
    detail_note: Option<String>,
}

enum Trailer {
    Tcp { extra: Option<Vec<u8>> },
    Rtu { pass: bool, lo: u8, hi: u8, crc: u16 },
    Ascii { lrc: u8 },
}

struct TransportResult {
    raw: Vec<u8>,
    pdu_off: usize,
    pdu_end: usize,
    unit: u8,
    trailer: Trailer,
}

fn u16be(b: &[u8], i: usize) -> u16 {
    u16::from_be_bytes([b[i], b[i + 1]])
}

fn plc_label(base: u32, addr: u16) -> String {
    format!("{:05}", base + addr as u32 + 1)
}

fn unit_word(unit: u8, lang: Lang) -> String {
    if unit == 0 {
        lang.pick("0 号从站（广播）".into(), "slave 0 (broadcast)".into())
    } else {
        lang.pick(format!("{unit} 号从站"), format!("slave {unit}"))
    }
}

fn quoted(name: &str, lang: Lang) -> String {
    lang.pick(format!("「{name}」"), format!("\"{name}\""))
}

fn qty_unit(info: &FcInfo, qty: usize, lang: Lang) -> String {
    lang.pick(format!("{qty} 个{}", info.unit_zh), {
        let word = if qty == 1 {
            info.unit_en
        } else {
            info.unit_en_plural
        };
        format!("{qty} {word}")
    })
}

fn display_width(s: &str) -> usize {
    s.chars()
        .map(|c| if (c as u32) >= 0x2E80 { 2 } else { 1 })
        .sum()
}

fn pad(s: &str, w: usize) -> String {
    let d = display_width(s);
    if d >= w {
        s.to_string()
    } else {
        format!("{}{}", s, " ".repeat(w - d))
    }
}

fn detect_transport(bytes: &[u8]) -> Option<&'static str> {
    if bytes.len() >= 8 && bytes[2] == 0 && bytes[3] == 0 {
        let declared = u16be(bytes, 4) as usize;
        if declared == bytes.len() - 6 {
            return Some("tcp");
        }
    }
    if bytes.len() >= 4 && tools::verify_crc16(bytes) {
        return Some("rtu");
    }
    None
}

fn guess_direction(fc: u8, data: &[u8], lang: Lang) -> (bool, Option<String>) {
    // Returns (is_response, note).
    let Some(info) = fc_info(fc) else {
        return (false, None);
    };
    let manual = lang.pick(
        "，已按请求解析，如不对请手动切换方向".into(),
        "; parsed as a request — switch the direction manually if that is wrong".into(),
    );
    match info.kind {
        Kind::Bits | Kind::Regs => {
            let is_req = data.len() == 4;
            let is_resp = !data.is_empty() && data[0] as usize == data.len() - 1;
            if is_req && !is_resp {
                (false, None)
            } else if is_resp && !is_req {
                (true, None)
            } else if is_req && is_resp {
                (
                    false,
                    Some(lang.pick(
                        format!("这条报文既符合请求也符合响应的结构{manual}"),
                        format!("This frame matches both request and response structures{manual}"),
                    )),
                )
            } else {
                (
                    false,
                    Some(lang.pick(
                        format!("无法从报文结构判断方向{manual}"),
                        format!("Could not determine the direction from the frame structure{manual}"),
                    )),
                )
            }
        }
        Kind::WriteSingleCoil | Kind::WriteSingleReg => {
            if data.len() == 4 {
                (
                    false,
                    Some(lang.pick(
                        format!("该功能码（05/06）的响应与请求字节完全相同{manual}"),
                        format!("FC05/06 responses echo the request byte-for-byte{manual}"),
                    )),
                )
            } else {
                (
                    false,
                    Some(lang.pick(
                        format!("无法从报文结构判断方向{manual}"),
                        format!("Could not determine the direction from the frame structure{manual}"),
                    )),
                )
            }
        }
        Kind::WriteMultiCoil | Kind::WriteMultiReg => {
            if data.len() == 4 {
                (true, None)
            } else if data.len() >= 5 {
                (false, None)
            } else {
                (
                    false,
                    Some(lang.pick(
                        format!("无法从报文结构判断方向{manual}"),
                        format!("Could not determine the direction from the frame structure{manual}"),
                    )),
                )
            }
        }
    }
}

fn parse_transport(
    data: &str,
    transport: &str,
    lang: Lang,
    fields: &mut Vec<Field>,
    tech: &mut Vec<String>,
    hints: &mut Vec<String>,
) -> Result<TransportResult, String> {
    if transport == "ascii" {
        let input = format!("{}\r\n", data.trim());
        let decoded = frame::decode_ascii(input.as_bytes()).map_err(|e| {
            lang.pick(
                format!("ASCII 报文解析失败：{e}"),
                format!("ASCII frame decode failed: {e}"),
            )
        })?;
        let unit = decoded.slave_id;
        let mut raw = Vec::with_capacity(decoded.pdu.len() + 2);
        raw.push(unit);
        raw.extend_from_slice(&decoded.pdu);
        let lrc = tools::lrc(&raw);
        raw.push(lrc);
        tech.push(lang.pick(
            format!("传输方式: Modbus ASCII（解码后共 {} 字节）", raw.len()),
            format!("Transport: Modbus ASCII ({} decoded bytes)", raw.len()),
        ));
        tech.push(lang.pick(
            format!("从站地址: {unit}"),
            format!("Slave address: {unit}"),
        ));
        fields.push(Field::new(
            &raw[0..1],
            lang.pick("从站地址".into(), "Slave address".into()),
            if unit == 0 {
                lang.pick(
                    "地址 0 = 广播，所有从站都接收".into(),
                    "address 0 = broadcast, received by all slaves".into(),
                )
            } else {
                lang.pick(format!("{unit} 号设备"), format!("device {unit}"))
            },
        ));
        let pdu_end = raw.len() - 1;
        return Ok(TransportResult {
            raw,
            pdu_off: 1,
            pdu_end,
            unit,
            trailer: Trailer::Ascii { lrc },
        });
    }

    let bytes = tools::parse_hex_string(data).map_err(|e| e.to_string())?;
    if bytes.is_empty() {
        return Err(lang.pick("请输入报文内容".into(), "Please enter frame data".into()));
    }
    let detected = match transport {
        "auto" => match detect_transport(&bytes) {
            Some("tcp") => {
                hints.push(lang.pick(
                    "已自动识别为 Modbus TCP：第 3~4 字节为 00 00，且长度字段与实际字节数吻合".into(),
                    "Auto-detected as Modbus TCP: bytes 3-4 are 00 00 and the length field matches the actual byte count".into(),
                ));
                "tcp"
            }
            Some(_) => {
                hints.push(lang.pick(
                    "已自动识别为 Modbus RTU：CRC16 校验通过".into(),
                    "Auto-detected as Modbus RTU: CRC16 check passed".into(),
                ));
                "rtu"
            }
            None => {
                return Err(lang.pick(
                    "无法自动识别报文格式：作为 Modbus TCP，第 3~4 字节应为 00 00 且长度字段要与实际字节数吻合；作为 Modbus RTU，CRC 校验没通过。请检查是否有缺字节，或手动指定协议。".into(),
                    "Could not auto-detect the frame format: as Modbus TCP, bytes 3-4 should be 00 00 with a matching length field; as Modbus RTU, the CRC check failed. Check for missing bytes or select the protocol manually.".into(),
                ));
            }
        },
        t => t,
    };

    if detected == "tcp" {
        if bytes.len() < 8 {
            return Err(lang.pick(
                format!(
                    "报文太短：Modbus TCP 至少需要 8 个字节（7 字节 MBAP 头 + 功能码），当前只有 {} 个",
                    bytes.len()
                ),
                format!(
                    "Frame too short: Modbus TCP needs at least 8 bytes (7-byte MBAP header + function code), got {}",
                    bytes.len()
                ),
            ));
        }
        let tx = u16be(&bytes, 0);
        let proto = u16be(&bytes, 2);
        let declared = u16be(&bytes, 4) as usize;
        let unit = bytes[6];
        let actual = bytes.len() - 6;
        tech.push(lang.pick(
            format!("传输方式: Modbus TCP（共 {} 字节）", bytes.len()),
            format!("Transport: Modbus TCP ({} bytes total)", bytes.len()),
        ));
        tech.push(lang.pick(
            format!("事务标识符: 0x{tx:04X}"),
            format!("Transaction ID: 0x{tx:04X}"),
        ));
        tech.push(lang.pick(
            format!("协议标识符: 0x{proto:04X}"),
            format!("Protocol ID: 0x{proto:04X}"),
        ));
        tech.push(lang.pick(
            format!("长度字段: {declared}（实际后续 {actual} 字节）"),
            format!("Length field: {declared} ({actual} bytes actually follow)"),
        ));
        tech.push(lang.pick(
            format!("单元标识符: {unit}"),
            format!("Unit ID: {unit}"),
        ));
        if proto != 0 {
            hints.push(lang.pick(
                format!("协议标识符是 0x{proto:04X}，标准 Modbus TCP 应该是 0x0000"),
                format!("The protocol identifier is 0x{proto:04X}; standard Modbus TCP uses 0x0000"),
            ));
        }
        if declared != actual {
            if declared > actual {
                hints.push(lang.pick(
                    format!("长度字段是 {declared}，但实际只收到 {actual} 个字节，报文可能被截断或长度填错"),
                    format!("The length field says {declared}, but only {actual} bytes follow; the frame may be truncated or the length is wrong"),
                ));
            } else {
                hints.push(lang.pick(
                    format!(
                        "长度字段是 {declared}，但实际收到 {actual} 个字节，多出的 {} 个可能属于下一条报文",
                        actual - declared
                    ),
                    format!(
                        "The length field says {declared}, but {actual} bytes follow; the extra {} bytes may belong to the next frame",
                        actual - declared
                    ),
                ));
            }
        }
        fields.push(Field::new(
            &bytes[0..2],
            lang.pick("事务标识符".into(), "Transaction identifier".into()),
            lang.pick(
                format!("主站给本次对话的编号 0x{tx:04X}，响应会原样带回"),
                format!("Sequence number 0x{tx:04X} assigned by the master to this exchange; the response echoes it"),
            ),
        ));
        fields.push(Field::new(
            &bytes[2..4],
            lang.pick("协议标识符".into(), "Protocol identifier".into()),
            if proto == 0 {
                lang.pick(
                    "0x0000 = 标准 Modbus 协议".into(),
                    "0x0000 = standard Modbus protocol".into(),
                )
            } else {
                lang.pick(
                    "标准 Modbus 应为 0x0000".into(),
                    "standard Modbus uses 0x0000".into(),
                )
            },
        ));
        fields.push(Field::new(
            &bytes[4..6],
            lang.pick("长度".into(), "Length".into()),
            lang.pick(
                format!(
                    "后面还有 {declared} 个字节（单元标识符 + 数据）{}",
                    if declared != actual {
                        format!("，但实际有 {actual} 个")
                    } else {
                        String::new()
                    }
                ),
                format!(
                    "{declared} more bytes follow (unit identifier + data){}",
                    if declared != actual {
                        format!(", but {actual} actually follow")
                    } else {
                        String::new()
                    }
                ),
            ),
        ));
        fields.push(Field::new(
            &bytes[6..7],
            lang.pick("单元标识符".into(), "Unit identifier".into()),
            lang.pick(
                format!("从站地址：{unit} 号设备"),
                format!("slave address: device {unit}"),
            ),
        ));
        let pdu_end = (6 + declared).clamp(7, bytes.len());
        let extra = if pdu_end < bytes.len() {
            Some(bytes[pdu_end..].to_vec())
        } else {
            None
        };
        Ok(TransportResult {
            raw: bytes,
            pdu_off: 7,
            pdu_end,
            unit,
            trailer: Trailer::Tcp { extra },
        })
    } else if detected == "rtu" {
        if bytes.len() < 4 {
            return Err(lang.pick(
                format!(
                    "报文太短：Modbus RTU 至少需要 4 个字节（地址 + 功能码 + 2 字节 CRC），当前只有 {} 个",
                    bytes.len()
                ),
                format!(
                    "Frame too short: Modbus RTU needs at least 4 bytes (address + function code + 2-byte CRC), got {}",
                    bytes.len()
                ),
            ));
        }
        let unit = bytes[0];
        let crc = tools::crc16(&bytes[..bytes.len() - 2]);
        let lo = (crc & 0xFF) as u8;
        let hi = (crc >> 8) as u8;
        let pass = lo == bytes[bytes.len() - 2] && hi == bytes[bytes.len() - 1];
        tech.push(lang.pick(
            format!("传输方式: Modbus RTU（共 {} 字节）", bytes.len()),
            format!("Transport: Modbus RTU ({} bytes total)", bytes.len()),
        ));
        tech.push(lang.pick(
            format!("从站地址: {unit}"),
            format!("Slave address: {unit}"),
        ));
        if pass {
            hints.push(lang.pick(
                "CRC 校验通过".into(),
                "CRC check passed".into(),
            ));
        } else {
            hints.push(lang.pick(
                format!(
                    "CRC 校验失败：报文末尾是 {:02X} {:02X}，按前面数据算出来应为 {lo:02X} {hi:02X}（低字节在前），报文可能有缺漏或被改动",
                    bytes[bytes.len() - 2],
                    bytes[bytes.len() - 1]
                ),
                format!(
                    "CRC check failed: the frame ends with {:02X} {:02X}, but the data computes to {lo:02X} {hi:02X} (low byte first); bytes may be missing or altered",
                    bytes[bytes.len() - 2],
                    bytes[bytes.len() - 1]
                ),
            ));
        }
        fields.push(Field::new(
            &bytes[0..1],
            lang.pick("从站地址".into(), "Slave address".into()),
            if unit == 0 {
                lang.pick(
                    "地址 0 = 广播，所有从站都接收".into(),
                    "address 0 = broadcast, received by all slaves".into(),
                )
            } else {
                lang.pick(format!("{unit} 号设备"), format!("device {unit}"))
            },
        ));
        let pdu_end = bytes.len() - 2;
        Ok(TransportResult {
            raw: bytes,
            pdu_off: 1,
            pdu_end,
            unit,
            trailer: Trailer::Rtu { pass, lo, hi, crc },
        })
    } else {
        Err(lang.pick(
            format!("不支持的传输方式：{detected}（可选 auto / tcp / rtu / ascii）"),
            format!("Unsupported transport: {detected} (use auto, tcp, rtu or ascii)"),
        ))
    }
}

fn parse_read_body(
    info: &FcInfo,
    fc_quoted: &str,
    data: &[u8],
    is_response: bool,
    unit: u8,
    bytes: &[u8],
    data_off: usize,
    lang: Lang,
    fields: &mut Vec<Field>,
    tech: &mut Vec<String>,
    hints: &mut Vec<String>,
) -> Body {
    let uw = unit_word(unit, lang);
    if !is_response {
        if data.len() != 4 {
            hints.push(lang.pick(
                format!(
                    "该功能码的请求数据区应为 4 个字节，实际 {} 个，报文可能不完整",
                    data.len()
                ),
                format!(
                    "The request data area for this function should be 4 bytes, got {}; the frame may be incomplete",
                    data.len()
                ),
            ));
        }
        if data.len() >= 4 {
            let start = u16be(data, 0);
            let qty = u16be(data, 2) as usize;
            let plc = plc_label(info.plc_base, start);
            fields.push(Field::new(
                &bytes[data_off..data_off + 2],
                lang.pick("起始地址".into(), "Start address".into()),
                lang.pick(
                    format!("协议地址 {start}，对应 PLC 地址 {plc}"),
                    format!("protocol address {start}, PLC address {plc}"),
                ),
            ));
            fields.push(Field::new(
                &bytes[data_off + 2..data_off + 4],
                lang.pick("读取数量".into(), "Read quantity".into()),
                lang.pick(
                    format!("读 {}", qty_unit(info, qty, lang)),
                    format!("read {}", qty_unit(info, qty, lang)),
                ),
            ));
            tech.push(lang.pick(
                format!("起始地址: {start}（PLC {plc}）"),
                format!("Start address: {start} (PLC {plc})"),
            ));
            tech.push(lang.pick(format!("数量: {qty}"), format!("Quantity: {qty}")));
            if qty < 1 || qty > info.max_qty as usize {
                hints.push(lang.pick(
                    format!("读取数量 {qty} 超出协议允许范围（1~{}）", info.max_qty),
                    format!("Read quantity {qty} is outside the protocol range (1-{})", info.max_qty),
                ));
            }
            return Body {
                summary: lang.pick(
                    format!(
                        "主站向 {uw}发起{fc_quoted}：从 {plc} 开始，读 {}",
                        qty_unit(info, qty, lang)
                    ),
                    format!(
                        "The master asks {uw} to {fc_quoted}: read {} starting from {plc}",
                        qty_unit(info, qty, lang)
                    ),
                ),
                detail_rows: vec![],
                detail_note: None,
            };
        }
        if !data.is_empty() {
            fields.push(Field::new(
                &bytes[data_off..data_off + data.len()],
                lang.pick("数据区".into(), "Data area".into()),
                lang.pick(
                    "数据不完整，无法解析起始地址和数量".into(),
                    "incomplete data; cannot parse the start address and quantity".into(),
                ),
            ));
        }
        return Body {
            summary: lang.pick(
                format!(
                    "主站向 {uw}发起{fc_quoted}，但数据区不完整（应 4 字节，实际 {} 字节）",
                    data.len()
                ),
                format!(
                    "The master asks {uw} to {fc_quoted}, but the data area is incomplete (should be 4 bytes, got {})",
                    data.len()
                ),
            ),
            detail_rows: vec![],
            detail_note: None,
        };
    }

    if data.is_empty() {
        hints.push(lang.pick(
            "响应缺少字节计数和数据".into(),
            "The response is missing the byte count and data".into(),
        ));
        return Body {
            summary: lang.pick(
                format!("{uw}回复{fc_quoted}，但数据区是空的"),
                format!("{uw} replied to {fc_quoted}, but the data area is empty"),
            ),
            detail_rows: vec![],
            detail_note: None,
        };
    }
    let byte_count = data[0] as usize;
    let payload = &data[1..];
    fields.push(Field::new(
        &bytes[data_off..data_off + 1],
        lang.pick("字节计数".into(), "Byte count".into()),
        lang.pick(
            format!("数据区共 {byte_count} 个字节"),
            format!("{byte_count} data bytes"),
        ),
    ));
    tech.push(lang.pick(
        format!("字节计数: {byte_count}"),
        format!("Byte count: {byte_count}"),
    ));
    if byte_count != payload.len() {
        hints.push(lang.pick(
            format!(
                "字节计数说数据区有 {byte_count} 个字节，实际跟着 {} 个，报文可能被截断或有多余字节",
                payload.len()
            ),
            format!(
                "The byte count says {byte_count} data bytes, but {} follow; the frame may be truncated or have extra bytes",
                payload.len()
            ),
        ));
    }
    let usable = &payload[..usable_len(byte_count, payload.len())];
    if usable.is_empty() {
        hints.push(lang.pick(
            "字节计数为 0，从站没有返回任何数据".into(),
            "The byte count is 0; the slave returned no data".into(),
        ));
        return Body {
            summary: lang.pick(
                format!("{uw}回复{fc_quoted}，但没有返回任何数据"),
                format!("{uw} replied to {fc_quoted}, but returned no data"),
            ),
            detail_rows: vec![],
            detail_note: None,
        };
    }
    fields.push(Field::new(
        &bytes[data_off + 1..data_off + 1 + usable.len()],
        lang.pick("数据".into(), "Data".into()),
        if info.kind == Kind::Bits {
            lang.pick(
                format!("每一位代表一个{}的状态（每个字节内低位在前）", info.unit_zh),
                format!("each bit is the state of one {} (LSB first within each byte)", info.unit_en),
            )
        } else {
            lang.pick(
                "每 2 个字节是一个寄存器的值".into(),
                "every 2 bytes are one register value".into(),
            )
        },
    ));
    tech.push(lang.pick(
        format!("数据: {}", tools::format_hex(usable, " ")),
        format!("Data: {}", tools::format_hex(usable, " ")),
    ));
    if info.kind == Kind::Bits {
        let mut rows = Vec::new();
        let mut on = 0usize;
        for (i, &byte) in usable.iter().enumerate() {
            for bit in 0..8 {
                let idx = i * 8 + bit;
                let is_on = (byte >> bit) & 1 == 1;
                if is_on {
                    on += 1;
                }
                rows.push(format!(
                    "{} = {}",
                    plc_label(info.plc_base, idx as u16),
                    if is_on { "ON" } else { "OFF" }
                ));
            }
        }
        let total = rows.len();
        let note = lang.pick(
            format!(
                "注：响应报文本身不含起始地址和数量，编号默认从 {} 起；最后一个字节可能带填充位，实际以对应请求为准",
                plc_label(info.plc_base, 0)
            ),
            format!(
                "Note: the response frame carries no start address or quantity; numbering starts at {} by default and the last byte may contain padding bits — refer to the matching request",
                plc_label(info.plc_base, 0)
            ),
        );
        Body {
            summary: lang.pick(
                format!(
                    "{uw}回复{fc_quoted}：共 {total} 位状态，ON {on} 个 / OFF {} 个（明细见下）",
                    total - on
                ),
                format!(
                    "{uw} replied to {fc_quoted}: {total} bit states, {on} ON / {} OFF (details below)",
                    total - on
                ),
            ),
            detail_rows: rows,
            detail_note: Some(note),
        }
    } else {
        if byte_count % 2 != 0 {
            hints.push(lang.pick(
                format!("寄存器值应为偶数个字节，字节计数 {byte_count} 是奇数，报文可能有误"),
                format!("Register values should be an even number of bytes, but the byte count {byte_count} is odd; the frame may be malformed"),
            ));
        }
        let mut rows = Vec::new();
        for (n, pair) in usable.chunks_exact(2).enumerate() {
            let value = u16::from_be_bytes([pair[0], pair[1]]);
            rows.push(lang.pick(
                format!("{} = {}（0x{value:04X}）", plc_label(info.plc_base, n as u16), value),
                format!("{} = {} (0x{value:04X})", plc_label(info.plc_base, n as u16), value),
            ));
        }
        let note = lang.pick(
            format!(
                "注：响应报文本身不含起始地址，编号默认从 {} 起，实际起始地址以对应请求为准",
                plc_label(info.plc_base, 0)
            ),
            format!(
                "Note: the response frame carries no start address; numbering starts at {} by default — the real start address comes from the matching request",
                plc_label(info.plc_base, 0)
            ),
        );
        let summary = if rows.len() == 1 {
            lang.pick(
                format!("{uw}回复{fc_quoted}：{}", rows[0]),
                format!("{uw} replied to {fc_quoted}: {}", rows[0]),
            )
        } else {
            lang.pick(
                format!(
                    "{uw}回复{fc_quoted}：共 {} 个寄存器，从 {} 开始（明细见下）",
                    rows.len(),
                    plc_label(info.plc_base, 0)
                ),
                format!(
                    "{uw} replied to {fc_quoted}: {} registers starting from {} (details below)",
                    rows.len(),
                    plc_label(info.plc_base, 0)
                ),
            )
        };
        Body {
            summary,
            detail_rows: rows,
            detail_note: Some(note),
        }
    }
}

fn usable_len(byte_count: usize, payload_len: usize) -> usize {
    byte_count.min(payload_len)
}

fn parse_write_single_body(
    info: &FcInfo,
    fc_quoted: &str,
    data: &[u8],
    is_response: bool,
    unit: u8,
    bytes: &[u8],
    data_off: usize,
    lang: Lang,
    fields: &mut Vec<Field>,
    tech: &mut Vec<String>,
    hints: &mut Vec<String>,
) -> Body {
    let uw = unit_word(unit, lang);
    if data.len() != 4 {
        hints.push(lang.pick(
            format!(
                "该功能码的数据区应为 4 个字节，实际 {} 个，报文可能不完整",
                data.len()
            ),
            format!(
                "The data area for this function should be 4 bytes, got {}; the frame may be incomplete",
                data.len()
            ),
        ));
    }
    if data.len() < 4 {
        if !data.is_empty() {
            fields.push(Field::new(
                &bytes[data_off..data_off + data.len()],
                lang.pick("数据区".into(), "Data area".into()),
                lang.pick("数据不完整".into(), "incomplete data".into()),
            ));
        }
        let head = if is_response {
            lang.pick(format!("{uw}回复{fc_quoted}"), format!("{uw} replied to {fc_quoted}"))
        } else {
            lang.pick(
                format!("主站向 {uw}发起{fc_quoted}"),
                format!("The master asks {uw} to {fc_quoted}"),
            )
        };
        return Body {
            summary: lang.pick(
                format!("{head}，但数据区不完整"),
                format!("{head}, but the data area is incomplete"),
            ),
            detail_rows: vec![],
            detail_note: None,
        };
    }
    let addr = u16be(data, 0);
    let val = u16be(data, 2);
    let plc = plc_label(info.plc_base, addr);
    fields.push(Field::new(
        &bytes[data_off..data_off + 2],
        lang.pick("地址".into(), "Address".into()),
        lang.pick(
            format!("协议地址 {addr}，对应 PLC 地址 {plc}"),
            format!("protocol address {addr}, PLC address {plc}"),
        ),
    ));
    tech.push(lang.pick(
        format!("地址: {addr}（PLC {plc}）"),
        format!("Address: {addr} (PLC {plc})"),
    ));
    if info.kind == Kind::WriteSingleCoil {
        let onoff = match val {
            0xFF00 => "ON",
            0x0000 => "OFF",
            _ => {
                hints.push(lang.pick(
                    format!("写线圈的值应为 0xFF00（ON）或 0x0000（OFF），实际是 0x{val:04X}"),
                    format!("A coil write value should be 0xFF00 (ON) or 0x0000 (OFF), got 0x{val:04X}"),
                ));
                if matches!(lang, Lang::Zh) {
                    "未知"
                } else {
                    "an unknown value"
                }
            }
        };
        fields.push(Field::new(
            &bytes[data_off + 2..data_off + 4],
            lang.pick("写入值".into(), "Write value".into()),
            if val == 0xFF00 || val == 0x0000 {
                format!("0x{val:04X} = {onoff}")
            } else {
                lang.pick(
                    format!("0x{val:04X}，不是标准值（0xFF00=ON，0x0000=OFF）"),
                    format!("0x{val:04X}, not a standard value (0xFF00=ON, 0x0000=OFF)"),
                )
            },
        ));
        tech.push(lang.pick(
            format!("写入值: 0x{val:04X}（{onoff}）"),
            format!("Write value: 0x{val:04X} ({onoff})"),
        ));
        let row = format!("{plc} = {}", if val == 0xFF00 { "ON" } else { "OFF" });
        Body {
            summary: if is_response {
                lang.pick(
                    format!("{uw}确认：线圈 {plc} 已设为 {onoff}"),
                    format!("{uw} confirmed: coil {plc} is now {onoff}"),
                )
            } else {
                lang.pick(
                    format!("主站向 {uw}发起{fc_quoted}：把线圈 {plc} 设为 {onoff}"),
                    format!("The master asks {uw} to {fc_quoted}: set coil {plc} to {onoff}"),
                )
            },
            detail_rows: vec![row],
            detail_note: None,
        }
    } else {
        fields.push(Field::new(
            &bytes[data_off + 2..data_off + 4],
            lang.pick("写入值".into(), "Write value".into()),
            lang.pick(
                format!("十进制 {val}，十六进制 0x{val:04X}"),
                format!("decimal {val}, hexadecimal 0x{val:04X}"),
            ),
        ));
        tech.push(lang.pick(
            format!("写入值: {val}（0x{val:04X}）"),
            format!("Write value: {val} (0x{val:04X})"),
        ));
        let row = lang.pick(
            format!("{plc} = {val}（0x{val:04X}）"),
            format!("{plc} = {val} (0x{val:04X})"),
        );
        Body {
            summary: if is_response {
                lang.pick(
                    format!("{uw}确认：寄存器 {plc} 已写为 {val}（0x{val:04X}）"),
                    format!("{uw} confirmed: register {plc} is now {val} (0x{val:04X})"),
                )
            } else {
                lang.pick(
                    format!("主站向 {uw}发起{fc_quoted}：把寄存器 {plc} 写为 {val}（0x{val:04X}）"),
                    format!("The master asks {uw} to {fc_quoted}: set register {plc} to {val} (0x{val:04X})"),
                )
            },
            detail_rows: vec![row],
            detail_note: None,
        }
    }
}

fn parse_write_multi_body(
    info: &FcInfo,
    fc_quoted: &str,
    data: &[u8],
    is_response: bool,
    unit: u8,
    bytes: &[u8],
    data_off: usize,
    lang: Lang,
    fields: &mut Vec<Field>,
    tech: &mut Vec<String>,
    hints: &mut Vec<String>,
) -> Body {
    let uw = unit_word(unit, lang);
    if is_response {
        if data.len() != 4 {
            hints.push(lang.pick(
                format!(
                    "该功能码的响应数据区应为 4 个字节，实际 {} 个",
                    data.len()
                ),
                format!(
                    "The response data area for this function should be 4 bytes, got {}",
                    data.len()
                ),
            ));
        }
        if data.len() < 4 {
            if !data.is_empty() {
                fields.push(Field::new(
                    &bytes[data_off..data_off + data.len()],
                    lang.pick("数据区".into(), "Data area".into()),
                    lang.pick("数据不完整".into(), "incomplete data".into()),
                ));
            }
            return Body {
                summary: lang.pick(
                    format!("{uw}回复{fc_quoted}，但数据区不完整"),
                    format!("{uw} replied to {fc_quoted}, but the data area is incomplete"),
                ),
                detail_rows: vec![],
                detail_note: None,
            };
        }
        let start = u16be(data, 0);
        let qty = u16be(data, 2) as usize;
        let plc = plc_label(info.plc_base, start);
        fields.push(Field::new(
            &bytes[data_off..data_off + 2],
            lang.pick("起始地址".into(), "Start address".into()),
            lang.pick(
                format!("协议地址 {start}，对应 PLC 地址 {plc}"),
                format!("protocol address {start}, PLC address {plc}"),
            ),
        ));
        fields.push(Field::new(
            &bytes[data_off + 2..data_off + 4],
            lang.pick("写入数量".into(), "Write quantity".into()),
            lang.pick(
                format!("成功写入 {}", qty_unit(info, qty, lang)),
                format!("successfully wrote {}", qty_unit(info, qty, lang)),
            ),
        ));
        tech.push(lang.pick(
            format!("起始地址: {start}（PLC {plc}）"),
            format!("Start address: {start} (PLC {plc})"),
        ));
        tech.push(lang.pick(
            format!("写入数量: {qty}"),
            format!("Write quantity: {qty}"),
        ));
        return Body {
            summary: lang.pick(
                format!("{uw}确认：已从 {plc} 开始写入 {}", qty_unit(info, qty, lang)),
                format!("{uw} confirmed: wrote {} starting from {plc}", qty_unit(info, qty, lang)),
            ),
            detail_rows: vec![],
            detail_note: None,
        };
    }

    if data.len() < 5 {
        hints.push(lang.pick(
            format!(
                "该功能码的请求数据区至少 5 个字节，实际 {} 个，报文不完整",
                data.len()
            ),
            format!(
                "The request data area for this function needs at least 5 bytes, got {}; the frame is incomplete",
                data.len()
            ),
        ));
        if !data.is_empty() {
            fields.push(Field::new(
                &bytes[data_off..data_off + data.len()],
                lang.pick("数据区".into(), "Data area".into()),
                lang.pick("数据不完整".into(), "incomplete data".into()),
            ));
        }
        return Body {
            summary: lang.pick(
                format!("主站向 {uw}发起{fc_quoted}，但数据区不完整"),
                format!("The master asks {uw} to {fc_quoted}, but the data area is incomplete"),
            ),
            detail_rows: vec![],
            detail_note: None,
        };
    }
    let start = u16be(data, 0);
    let qty = u16be(data, 2) as usize;
    let byte_count = data[4] as usize;
    let payload = &data[5..];
    let plc = plc_label(info.plc_base, start);
    fields.push(Field::new(
        &bytes[data_off..data_off + 2],
        lang.pick("起始地址".into(), "Start address".into()),
        lang.pick(
            format!("协议地址 {start}，对应 PLC 地址 {plc}"),
            format!("protocol address {start}, PLC address {plc}"),
        ),
    ));
    fields.push(Field::new(
        &bytes[data_off + 2..data_off + 4],
        lang.pick("写入数量".into(), "Write quantity".into()),
        lang.pick(
            format!("写 {}", qty_unit(info, qty, lang)),
            format!("write {}", qty_unit(info, qty, lang)),
        ),
    ));
    fields.push(Field::new(
        &bytes[data_off + 4..data_off + 5],
        lang.pick("字节计数".into(), "Byte count".into()),
        lang.pick(
            format!("写入数据共 {byte_count} 个字节"),
            format!("{byte_count} bytes of write data"),
        ),
    ));
    tech.push(lang.pick(
        format!("起始地址: {start}（PLC {plc}）"),
        format!("Start address: {start} (PLC {plc})"),
    ));
    tech.push(lang.pick(
        format!("写入数量: {qty}"),
        format!("Write quantity: {qty}"),
    ));
    tech.push(lang.pick(
        format!("字节计数: {byte_count}"),
        format!("Byte count: {byte_count}"),
    ));
    if qty < 1 || qty > info.max_qty as usize {
        hints.push(lang.pick(
            format!("写入数量 {qty} 超出协议允许范围（1~{}）", info.max_qty),
            format!("Write quantity {qty} is outside the protocol range (1-{})", info.max_qty),
        ));
    }
    let expected = if info.kind == Kind::WriteMultiCoil {
        qty.div_ceil(8)
    } else {
        qty * 2
    };
    if byte_count != expected {
        hints.push(lang.pick(
            format!(
                "要写 {}，数据应是 {expected} 个字节，字节计数却写 {byte_count}",
                qty_unit(info, qty, lang)
            ),
            format!(
                "Writing {} needs {expected} data bytes, but the byte count says {byte_count}",
                qty_unit(info, qty, lang)
            ),
        ));
    }
    if byte_count != payload.len() {
        hints.push(lang.pick(
            format!(
                "字节计数说后面有 {byte_count} 个字节，实际跟着 {} 个",
                payload.len()
            ),
            format!(
                "The byte count says {byte_count} bytes follow, but {} actually follow",
                payload.len()
            ),
        ));
    }
    let usable = &payload[..usable_len(byte_count, payload.len())];
    if !usable.is_empty() {
        fields.push(Field::new(
            &bytes[data_off + 5..data_off + 5 + usable.len()],
            lang.pick("写入数据".into(), "Write data".into()),
            if info.kind == Kind::WriteMultiCoil {
                lang.pick(
                    "每一位代表一个线圈要写的状态（每个字节内低位在前）".into(),
                    "each bit is the state to write to one coil (LSB first within each byte)".into(),
                )
            } else {
                lang.pick(
                    "每 2 个字节是一个寄存器要写的值".into(),
                    "every 2 bytes are one register value to write".into(),
                )
            },
        ));
        tech.push(lang.pick(
            format!("数据: {}", tools::format_hex(usable, " ")),
            format!("Data: {}", tools::format_hex(usable, " ")),
        ));
    }
    if info.kind == Kind::WriteMultiCoil {
        let mut rows = Vec::new();
        let mut on = 0usize;
        'outer: for (i, &byte) in usable.iter().enumerate() {
            for bit in 0..8 {
                if rows.len() >= qty {
                    break 'outer;
                }
                let is_on = (byte >> bit) & 1 == 1;
                if is_on {
                    on += 1;
                }
                let idx = start as usize + i * 8 + bit;
                rows.push(format!(
                    "{} = {}",
                    plc_label(info.plc_base, idx as u16),
                    if is_on { "ON" } else { "OFF" }
                ));
            }
        }
        Body {
            summary: lang.pick(
                format!(
                    "主站向 {uw}发起{fc_quoted}：从 {plc} 开始写 {}（ON {on} 个 / OFF {} 个）",
                    qty_unit(info, qty, lang),
                    rows.len() - on
                ),
                format!(
                    "The master asks {uw} to {fc_quoted}: write {} starting from {plc} ({on} ON / {} OFF)",
                    qty_unit(info, qty, lang),
                    rows.len() - on
                ),
            ),
            detail_rows: rows,
            detail_note: None,
        }
    } else {
        let mut rows = Vec::new();
        for pair in usable.chunks_exact(2) {
            if rows.len() >= qty {
                break;
            }
            let value = u16::from_be_bytes([pair[0], pair[1]]);
            let idx = start as usize + rows.len();
            rows.push(lang.pick(
                format!("{} = {}（0x{value:04X}）", plc_label(info.plc_base, idx as u16), value),
                format!("{} = {} (0x{value:04X})", plc_label(info.plc_base, idx as u16), value),
            ));
        }
        Body {
            summary: lang.pick(
                format!(
                    "主站向 {uw}发起{fc_quoted}：从 {plc} 开始写 {}",
                    qty_unit(info, qty, lang)
                ),
                format!(
                    "The master asks {uw} to {fc_quoted}: write {} starting from {plc}",
                    qty_unit(info, qty, lang)
                ),
            ),
            detail_rows: rows,
            detail_note: None,
        }
    }
}

pub fn explain(data: &str, transport: &str, direction: &str, lang: Lang) -> Result<String, String> {
    if !matches!(direction, "auto" | "request" | "response") {
        return Err(lang.pick(
            format!("方向参数无效：{direction}（可选 auto / request / response）"),
            format!("Invalid direction: {direction} (use auto, request or response)"),
        ));
    }

    let mut hints: Vec<String> = Vec::new();
    let mut fields: Vec<Field> = Vec::new();
    let mut tech: Vec<String> = Vec::new();

    let tr = parse_transport(data, transport, lang, &mut fields, &mut tech, &mut hints)?;
    let pdu = tr.raw[tr.pdu_off..tr.pdu_end].to_vec();
    if pdu.is_empty() {
        return Err(lang.pick(
            "报文里没有 PDU（缺少功能码）".into(),
            "The frame contains no PDU (missing function code)".into(),
        ));
    }
    let fc_byte = pdu[0];
    let is_exception = fc_byte & 0x80 != 0;
    let fc = fc_byte & 0x7F;
    let body = &pdu[1..];
    let body_off = tr.pdu_off + 1;
    let unit = tr.unit;
    let uw = unit_word(unit, lang);

    let info = fc_info(fc);
    let fc_display = match &info {
        Some(i) => lang.pick(i.zh.into(), i.en.into()),
        None => lang.pick(format!("功能码 0x{fc:02X}"), format!("function 0x{fc:02X}")),
    };
    let fc_quoted = quoted(&fc_display, lang);

    let is_response = if is_exception {
        true
    } else if direction != "auto" {
        direction == "response"
    } else {
        let (resp, note) = guess_direction(fc, body, lang);
        if let Some(n) = note {
            hints.push(n);
        }
        resp
    };

    // Function code row.
    let fc_label = lang.pick("功能码".into(), "Function code".into());
    if is_exception {
        fields.push(Field::new(
            &tr.raw[tr.pdu_off..tr.pdu_off + 1],
            fc_label,
            lang.pick(
                format!("0x{fc_byte:02X} = 0x{fc:02X} + 0x80，表示{fc_quoted}出了异常"),
                format!("0x{fc_byte:02X} = 0x{fc:02X} + 0x80, meaning {fc_quoted} raised an exception"),
            ),
        ));
        tech.push(lang.pick(
            format!("功能码: 0x{fc_byte:02X}（0x{fc:02X} | 0x80，异常响应）"),
            format!("Function: 0x{fc_byte:02X} (0x{fc:02X} | 0x80, exception response)"),
        ));
    } else {
        match &info {
            Some(i) => {
                fields.push(Field::new(
                    &tr.raw[tr.pdu_off..tr.pdu_off + 1],
                    fc_label,
                    lang.pick(
                        format!("0x{fc_byte:02X} = {}（{}）", i.zh, i.en),
                        format!("0x{fc_byte:02X} = {}", i.en),
                    ),
                ));
                tech.push(lang.pick(
                    format!("功能码: 0x{fc_byte:02X}（{} / {}）", i.en, i.zh),
                    format!("Function: 0x{fc_byte:02X} ({})", i.en),
                ));
            }
            None => {
                fields.push(Field::new(
                    &tr.raw[tr.pdu_off..tr.pdu_off + 1],
                    fc_label,
                    lang.pick(
                        format!("0x{fc_byte:02X}，暂不支持详细解析"),
                        format!("0x{fc_byte:02X}; detailed parsing not supported yet"),
                    ),
                ));
                tech.push(lang.pick(
                    format!("功能码: 0x{fc_byte:02X}"),
                    format!("Function: 0x{fc_byte:02X}"),
                ));
            }
        }
    }

    let parsed: Body = if is_exception {
        if body.is_empty() {
            hints.push(lang.pick(
                "异常响应缺少异常码字节".into(),
                "The exception response is missing the exception code byte".into(),
            ));
            Body {
                summary: lang.pick(
                    format!("{uw}拒绝了{fc_quoted}请求，但异常码缺失"),
                    format!("{uw} rejected the {fc_quoted} request, but the exception code is missing"),
                ),
                detail_rows: vec![],
                detail_note: None,
            }
        } else {
            let code = body[0];
            let ex = exception_info(code, lang);
            fields.push(Field::new(
                &tr.raw[body_off..body_off + 1],
                lang.pick("异常码".into(), "Exception code".into()),
                match ex {
                    Some((name, desc)) => lang.pick(
                        format!("0x{code:02X} = {name} —— {desc}"),
                        format!("0x{code:02X} = {name} — {desc}"),
                    ),
                    None => lang.pick(
                        format!("0x{code:02X}，未知异常码"),
                        format!("0x{code:02X}, unknown exception code"),
                    ),
                },
            ));
            if let Some((name, _)) = ex {
                tech.push(lang.pick(
                    format!("异常码: 0x{code:02X}（{name}）"),
                    format!("Exception code: 0x{code:02X} ({name})"),
                ));
            } else {
                tech.push(lang.pick(
                    format!("异常码: 0x{code:02X}"),
                    format!("Exception code: 0x{code:02X}"),
                ));
            }
            if body.len() > 1 {
                hints.push(lang.pick(
                    format!(
                        "异常响应通常只带 1 个异常码字节，后面多出 {} 个字节，请留意",
                        body.len() - 1
                    ),
                    format!(
                        "An exception response usually carries exactly 1 exception-code byte; {} extra byte(s) follow",
                        body.len() - 1
                    ),
                ));
            }
            Body {
                summary: match ex {
                    Some((name, desc)) => lang.pick(
                        format!("{uw}拒绝了{fc_quoted}请求：{name} —— {desc}"),
                        format!("{uw} rejected the {fc_quoted} request: {name} — {desc}"),
                    ),
                    None => lang.pick(
                        format!("{uw}拒绝了{fc_quoted}请求：未知异常码 0x{code:02X}"),
                        format!("{uw} rejected the {fc_quoted} request: unknown exception code 0x{code:02X}"),
                    ),
                },
                detail_rows: vec![],
                detail_note: None,
            }
        }
    } else {
        match &info {
            None => {
                if !body.is_empty() {
                    fields.push(Field::new(
                        &tr.raw[body_off..body_off + body.len()],
                        lang.pick("数据区".into(), "Data area".into()),
                        lang.pick(
                            format!("共 {} 字节，该功能码暂不支持详细解析", body.len()),
                            format!("{} bytes; detailed parsing not supported for this function yet", body.len()),
                        ),
                    ));
                    tech.push(lang.pick(
                        format!("数据: {}", tools::format_hex(body, " ")),
                        format!("Data: {}", tools::format_hex(body, " ")),
                    ));
                }
                let head = if is_response {
                    lang.pick(
                        format!("{uw}回复的{fc_quoted}"),
                        format!("{fc_quoted} replied by {uw}"),
                    )
                } else {
                    lang.pick(
                        format!("主站发给 {uw} 的{fc_quoted}"),
                        format!("{fc_quoted} from the master to {uw}"),
                    )
                };
                Body {
                    summary: lang.pick(
                        format!("{head}：暂不支持详细解析，仅展示基本结构"),
                        format!("{head}: detailed parsing not supported yet; showing the basic structure only"),
                    ),
                    detail_rows: vec![],
                    detail_note: None,
                }
            }
            Some(i) => match i.kind {
                Kind::Bits | Kind::Regs => parse_read_body(
                    i,
                    &fc_quoted,
                    body,
                    is_response,
                    unit,
                    &tr.raw,
                    body_off,
                    lang,
                    &mut fields,
                    &mut tech,
                    &mut hints,
                ),
                Kind::WriteSingleCoil | Kind::WriteSingleReg => parse_write_single_body(
                    i,
                    &fc_quoted,
                    body,
                    is_response,
                    unit,
                    &tr.raw,
                    body_off,
                    lang,
                    &mut fields,
                    &mut tech,
                    &mut hints,
                ),
                Kind::WriteMultiCoil | Kind::WriteMultiReg => parse_write_multi_body(
                    i,
                    &fc_quoted,
                    body,
                    is_response,
                    unit,
                    &tr.raw,
                    body_off,
                    lang,
                    &mut fields,
                    &mut tech,
                    &mut hints,
                ),
            },
        }
    };

    match &tr.trailer {
        Trailer::Tcp { extra } => {
            if let Some(extra) = extra {
                fields.push(Field::new(
                    extra,
                    lang.pick("多余字节".into(), "Extra bytes".into()),
                    lang.pick(
                        "超出长度字段声明的部分，可能是下一条报文的开头".into(),
                        "bytes beyond the declared length, possibly the start of the next frame".into(),
                    ),
                ));
            }
        }
        Trailer::Rtu { pass, lo, hi, crc } => {
            let n = tr.raw.len();
            fields.push(Field::new(
                &tr.raw[n - 2..],
                lang.pick("CRC 校验".into(), "CRC check".into()),
                if *pass {
                    lang.pick("CRC16 校验通过".into(), "CRC16 check passed".into())
                } else {
                    lang.pick(
                        format!("校验失败，按数据算出应为 {lo:02X} {hi:02X}"),
                        format!("check failed; the data computes to {lo:02X} {hi:02X}"),
                    )
                },
            ));
            tech.push(lang.pick(
                format!(
                    "CRC16: 0x{crc:04X}（线上字节序 {lo:02X} {hi:02X}，{}）",
                    if *pass { "校验通过" } else { "校验失败" }
                ),
                format!(
                    "CRC16: 0x{crc:04X} (wire order {lo:02X} {hi:02X}, {})",
                    if *pass { "passed" } else { "failed" }
                ),
            ));
        }
        Trailer::Ascii { lrc } => {
            let n = tr.raw.len();
            fields.push(Field::new(
                &tr.raw[n - 1..],
                lang.pick("LRC 校验".into(), "LRC check".into()),
                lang.pick("LRC 校验通过".into(), "LRC check passed".into()),
            ));
            tech.push(lang.pick(
                format!("LRC: 0x{lrc:02X}（校验通过）"),
                format!("LRC: 0x{lrc:02X} (passed)"),
            ));
        }
    }
    tech.push(format!("PDU: {}", tools::format_hex(&pdu, " ")));

    render(&parsed, &fields, &hints, &tech, lang)
}

fn render(parsed: &Body, fields: &[Field], hints: &[String], tech: &[String], lang: Lang) -> Result<String, String> {
    let mut out = parsed.summary.clone();
    let section = |title: &str| format!("\n\n── {title} ──\n");

    if !parsed.detail_rows.is_empty() {
        out.push_str(&section(&lang.pick("数据明细".into(), "Data details".into())));
        for row in &parsed.detail_rows {
            out.push_str(row);
            out.push('\n');
        }
        if let Some(note) = &parsed.detail_note {
            out.push_str(note);
            out.push('\n');
        }
        out.pop();
    }

    out.push_str(&section(&lang.pick("逐字节对照".into(), "Byte-by-byte".into())));
    let w1 = fields.iter().map(|f| display_width(&f.hex)).max().unwrap_or(0);
    let w2 = fields.iter().map(|f| display_width(&f.label)).max().unwrap_or(0);
    let rows: Vec<String> = fields
        .iter()
        .map(|f| format!("{}  {}  {}", pad(&f.hex, w1), pad(&f.label, w2), f.desc))
        .collect();
    out.push_str(&rows.join("\n"));

    if !hints.is_empty() {
        out.push_str(&section(&lang.pick("提示".into(), "Notes".into())));
        out.push_str(&hints.join("\n"));
    }

    out.push_str(&section(&lang.pick("技术细节".into(), "Technical details".into())));
    out.push_str(&tech.join("\n"));
    Ok(out)
}
