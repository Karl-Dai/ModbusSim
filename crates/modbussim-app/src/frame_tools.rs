use crate::frame_explain::{self, Lang};

#[tauri::command]
pub fn inspect_modbus_frame(
    data: String,
    transport: String,
    direction: String,
    lang: String,
) -> Result<String, String> {
    frame_explain::explain(&data, &transport, &direction, Lang::from_code(&lang))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn run(data: &str, transport: &str, direction: &str, lang: &str) -> Result<String, String> {
        inspect_modbus_frame(data.into(), transport.into(), direction.into(), lang.into())
    }

    #[test]
    fn csv_unsigned_type_names_accept_canonical_and_legacy_spellings() {
        use modbussim_core::register::DataType;
        for name in ["uint16", "u_int16"] {
            assert_eq!(
                serde_json::from_value::<DataType>(serde_json::json!(name)).unwrap(),
                DataType::UInt16
            );
        }
        assert_eq!(serde_json::to_value(DataType::UInt16).unwrap(), "u_int16");
    }

    #[test]
    fn auto_detects_tcp_and_explains_request_in_chinese() {
        let out = run("00 01 00 00 00 06 01 03 00 00 00 01", "auto", "auto", "zh").unwrap();
        assert!(out.contains("Modbus TCP"), "{out}");
        assert!(out.contains("主站向 1 号从站发起「读保持寄存器」：从 40001 开始，读 1 个寄存器"), "{out}");
    }

    #[test]
    fn auto_detects_rtu_via_crc() {
        let out = run("01 03 00 00 00 0A C5 CD", "auto", "auto", "zh").unwrap();
        assert!(out.contains("Modbus RTU"), "{out}");
        assert!(out.contains("读 10 个寄存器"), "{out}");
        assert!(out.contains("CRC16 校验通过"), "{out}");
    }

    #[test]
    fn auto_detects_response_direction() {
        let out = run("00 01 00 00 00 05 01 03 02 00 0A", "auto", "auto", "zh").unwrap();
        assert!(out.contains("1 号从站回复「读保持寄存器」：40001 = 10（0x000A）"), "{out}");
    }

    #[test]
    fn translates_exception_codes() {
        let out = run("00 01 00 00 00 03 01 83 02", "auto", "auto", "zh").unwrap();
        assert!(out.contains("1 号从站拒绝了「读保持寄存器」请求：非法数据地址"), "{out}");
        let en = run("00 01 00 00 00 03 01 83 02", "auto", "auto", "en").unwrap();
        assert!(en.contains("Illegal data address"), "{en}");
    }

    #[test]
    fn mbap_length_mismatch_warns_but_still_parses() {
        let out = run("00 01 00 00 00 07 01 03 00 00 00 01", "tcp", "auto", "zh").unwrap();
        assert!(out.contains("长度字段是 7，但实际只收到 6 个字节"), "{out}");
        assert!(out.contains("读保持寄存器"), "{out}");
    }

    #[test]
    fn english_output_uses_english_wording() {
        let out = run("00 01 00 00 00 06 01 03 00 00 00 01", "auto", "auto", "en").unwrap();
        assert!(out.contains("Read Holding Registers"), "{out}");
        assert!(out.contains("slave 1"), "{out}");
        assert!(out.contains("Byte-by-byte"), "{out}");
    }

    #[test]
    fn rtu_crc_failure_warns_but_still_parses() {
        let out = run("01 03 00 00 00 01 00 00", "rtu", "auto", "zh").unwrap();
        assert!(out.contains("CRC 校验失败"), "{out}");
        assert!(out.contains("读保持寄存器"), "{out}");
        assert!(run("01 03 00 00 00 01 00 00", "auto", "auto", "zh").is_err());
    }

    #[test]
    fn rtu_roundtrip_still_works() {
        let valid = modbussim_core::frame::encode_rtu(1, &[3, 0, 0, 0, 1]);
        let hex = valid.iter().map(|b| format!("{b:02X}")).collect::<String>();
        assert!(run(&hex, "rtu", "request", "zh").is_ok());
    }

    #[test]
    fn write_single_coil_summary_is_human_readable() {
        let out = run("00 02 00 00 00 06 01 05 00 00 FF 00", "tcp", "request", "zh").unwrap();
        assert!(out.contains("把线圈 00001 设为 ON"), "{out}");
    }

    #[test]
    fn ascii_frames_get_the_same_treatment() {
        let frame = modbussim_core::frame::encode_ascii(1, &[3, 0, 0, 0, 1]);
        let text = String::from_utf8(frame).unwrap();
        let out = run(text.trim(), "ascii", "request", "zh").unwrap();
        assert!(out.contains("Modbus ASCII"), "{out}");
        assert!(out.contains("读保持寄存器"), "{out}");
        assert!(out.contains("LRC 校验通过"), "{out}");
    }
}
