use modbussim_core::{frame, pdu, tools};

#[tauri::command]
pub fn inspect_modbus_frame(
    data: String,
    transport: String,
    direction: String,
) -> Result<String, String> {
    let (unit, pdu, header) = match transport.as_str() {
        "ascii" => {
            let input = format!("{}\r\n", data.trim());
            let decoded = frame::decode_ascii(input.as_bytes())?;
            (decoded.slave_id, decoded.pdu, "ASCII · LRC OK".to_string())
        }
        "rtu" => {
            let bytes = tools::parse_hex_string(&data).map_err(|e| e.to_string())?;
            let decoded = frame::decode_rtu(&bytes)?;
            (decoded.slave_id, decoded.pdu, "RTU · CRC16 OK".to_string())
        }
        "tcp" => {
            let bytes = tools::parse_hex_string(&data).map_err(|e| e.to_string())?;
            if bytes.len() < 8 {
                return Err("TCP ADU requires at least 8 bytes".into());
            }
            let transaction = u16::from_be_bytes([bytes[0], bytes[1]]);
            let protocol = u16::from_be_bytes([bytes[2], bytes[3]]);
            let length = u16::from_be_bytes([bytes[4], bytes[5]]) as usize;
            if protocol != 0 || !(2..=254).contains(&length) || bytes.len() != length + 6 {
                return Err(
                    "Invalid MBAP protocol/length; paste one complete Modbus TCP ADU".into(),
                );
            }
            (
                bytes[6],
                bytes[7..].to_vec(),
                format!("TCP · Transaction {transaction} · Length {length}"),
            )
        }
        _ => return Err("Unsupported transport".into()),
    };
    let fc = *pdu.first().ok_or("Empty PDU")?;
    let details = match direction.as_str() {
        "request" => format!(
            "{:?}",
            pdu::parse_request_pdu(&pdu).map_err(|e| e.to_string())?
        ),
        "response" if fc & 0x80 != 0 && pdu.len() == 2 => format!("Exception: 0x{:02X}", pdu[1]),
        "response" if (1..=4).contains(&fc) => {
            if pdu.len() < 2 || pdu.len() != pdu[1] as usize + 2 || (fc >= 3 && pdu[1] % 2 != 0) {
                return Err("Invalid read response byte count".into());
            }
            if fc >= 3 {
                format!(
                    "Registers: {:?}",
                    pdu[2..]
                        .chunks_exact(2)
                        .map(|pair| u16::from_be_bytes([pair[0], pair[1]]))
                        .collect::<Vec<_>>()
                )
            } else {
                format!("Packed bits: {:02X?}", &pdu[2..])
            }
        }
        "response" if [5, 6, 15, 16].contains(&fc) && pdu.len() == 5 => {
            format!(
                "Address: {} · Value/quantity: {}",
                u16::from_be_bytes([pdu[1], pdu[2]]),
                u16::from_be_bytes([pdu[3], pdu[4]])
            )
        }
        "response" => return Err("Unsupported or invalid response PDU".into()),
        _ => return Err("Choose request or response".into()),
    };
    Ok(format!(
        "{header}\nUnit ID: {unit}\nFC: 0x{fc:02X}\n{details}\nPDU: {:02X?}",
        pdu
    ))
}

#[cfg(test)]
mod tests {
    use super::*;
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
    fn validates_mbap_and_direction() {
        let request = "000100000006010300000001";
        assert!(
            inspect_modbus_frame(request.into(), "tcp".into(), "request".into())
                .unwrap()
                .contains("ReadHoldingRegisters")
        );
        assert!(inspect_modbus_frame(request.into(), "tcp".into(), "response".into()).is_err());
        assert!(inspect_modbus_frame(
            "000100000007010300000001".into(),
            "tcp".into(),
            "request".into()
        )
        .is_err());
        assert!(inspect_modbus_frame(
            "000100000005010302002A".into(),
            "tcp".into(),
            "response".into()
        )
        .unwrap()
        .contains("42"));
        let valid = frame::encode_rtu(1, &[3, 0, 0, 0, 1]);
        let hex = valid.iter().map(|b| format!("{b:02X}")).collect::<String>();
        assert!(inspect_modbus_frame(hex, "rtu".into(), "request".into()).is_ok());
        assert!(
            inspect_modbus_frame("0103000000010000".into(), "rtu".into(), "request".into())
                .is_err()
        );
    }
}
