package explain

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Karl-Dai/ModbusSim/go/internal/frame"
	"github.com/Karl-Dai/ModbusSim/go/internal/tools"
)

// detectTransport reports "tcp" or "rtu" when the bytes look like one, and ""
// when neither reading fits.
func detectTransport(bytes []byte) string {
	if len(bytes) >= 8 && bytes[2] == 0 && bytes[3] == 0 {
		if int(u16BE(bytes, 4)) == len(bytes)-6 {
			return "tcp"
		}
	}
	if len(bytes) >= 4 && tools.VerifyCRC16(bytes) {
		return "rtu"
	}
	return ""
}

// guessDirection returns (isResponse, hint). An empty hint means the
// structure decided without ambiguity.
func guessDirection(fc byte, data []byte, lang Lang) (bool, string) {
	info, known := fcInfoFor(fc)
	if !known {
		return false, ""
	}
	manual := lang.pick(
		"，已按请求解析，如不对请手动切换方向",
		"; parsed as a request — switch the direction manually if that is wrong")
	undecided := lang.pick(
		fmt.Sprintf("无法从报文结构判断方向%s", manual),
		fmt.Sprintf("Could not determine the direction from the frame structure%s", manual))

	switch info.kind {
	case kindBits, kindRegs:
		isReq := len(data) == 4
		isResp := len(data) > 0 && int(data[0]) == len(data)-1
		switch {
		case isReq && !isResp:
			return false, ""
		case isResp && !isReq:
			return true, ""
		case isReq && isResp:
			return false, lang.pick(
				fmt.Sprintf("这条报文既符合请求也符合响应的结构%s", manual),
				fmt.Sprintf("This frame matches both request and response structures%s", manual))
		default:
			return false, undecided
		}
	case kindWriteSingleCoil, kindWriteSingleReg:
		if len(data) == 4 {
			return false, lang.pick(
				fmt.Sprintf("该功能码（05/06）的响应与请求字节完全相同%s", manual),
				fmt.Sprintf("FC05/06 responses echo the request byte-for-byte%s", manual))
		}
		return false, undecided
	default:
		if len(data) == 4 {
			return true, ""
		}
		if len(data) >= 5 {
			return false, ""
		}
		return false, undecided
	}
}

func parseTransport(data, transport string, lang Lang, rep *report) (transportResult, error) {
	if transport == "ascii" {
		input := strings.TrimSpace(data) + "\r\n"
		decoded, err := frame.DecodeASCII([]byte(input))
		if err != nil {
			return transportResult{}, errors.New(lang.pick(
				fmt.Sprintf("ASCII 报文解析失败：%v", err),
				fmt.Sprintf("ASCII frame decode failed: %v", err)))
		}
		unit := decoded.SlaveID
		raw := make([]byte, 0, len(decoded.PDU)+2)
		raw = append(raw, unit)
		raw = append(raw, decoded.PDU...)
		lrc := tools.LRC(raw)
		raw = append(raw, lrc)
		rep.pushTech(lang.pick(
			fmt.Sprintf("传输方式: Modbus ASCII（解码后共 %d 字节）", len(raw)),
			fmt.Sprintf("Transport: Modbus ASCII (%d decoded bytes)", len(raw))))
		rep.pushTech(lang.pick(
			fmt.Sprintf("从站地址: %d", unit),
			fmt.Sprintf("Slave address: %d", unit)))
		rep.fieldAt(raw, 0, 1, lang.pick("从站地址", "Slave address"), slaveAddrWord(unit, lang))
		return transportResult{raw: raw, pduOff: 1, pduEnd: len(raw) - 1, unit: unit,
			trailer: trailer{kind: trailerASCII, lrc: lrc}}, nil
	}

	// Rust maps the parse error with e.to_string(), so the text is verbatim.
	bytes, err := tools.ParseHexString(data)
	if err != nil {
		return transportResult{}, err
	}
	if len(bytes) == 0 {
		return transportResult{}, errors.New(lang.pick("请输入报文内容", "Please enter frame data"))
	}

	detected := transport
	if transport == "auto" {
		switch detectTransport(bytes) {
		case "tcp":
			rep.pushHint(lang.pick(
				"已自动识别为 Modbus TCP：第 3~4 字节为 00 00，且长度字段与实际字节数吻合",
				"Auto-detected as Modbus TCP: bytes 3-4 are 00 00 and the length field matches the actual byte count"))
			detected = "tcp"
		case "rtu":
			rep.pushHint(lang.pick("已自动识别为 Modbus RTU：CRC16 校验通过",
				"Auto-detected as Modbus RTU: CRC16 check passed"))
			detected = "rtu"
		default:
			return transportResult{}, errors.New(lang.pick(
				"无法自动识别报文格式：作为 Modbus TCP，第 3~4 字节应为 00 00 且长度字段要与实际字节数吻合；作为 Modbus RTU，CRC 校验没通过。请检查是否有缺字节，或手动指定协议。",
				"Could not auto-detect the frame format: as Modbus TCP, bytes 3-4 should be 00 00 with a matching length field; as Modbus RTU, the CRC check failed. Check for missing bytes or select the protocol manually."))
		}
	}

	switch detected {
	case "tcp":
		return parseTCP(bytes, lang, rep)
	case "rtu":
		return parseRTU(bytes, lang, rep)
	}
	return transportResult{}, errors.New(lang.pick(
		fmt.Sprintf("不支持的传输方式：%s（可选 auto / tcp / rtu / ascii）", detected),
		fmt.Sprintf("Unsupported transport: %s (use auto, tcp, rtu or ascii)", detected)))
}

func slaveAddrWord(unit byte, lang Lang) string {
	if unit == 0 {
		return lang.pick("地址 0 = 广播，所有从站都接收", "address 0 = broadcast, received by all slaves")
	}
	return lang.pick(fmt.Sprintf("%d 号设备", unit), fmt.Sprintf("device %d", unit))
}

func parseTCP(bytes []byte, lang Lang, rep *report) (transportResult, error) {
	if len(bytes) < 8 {
		return transportResult{}, errors.New(lang.pick(
			fmt.Sprintf("报文太短：Modbus TCP 至少需要 8 个字节（7 字节 MBAP 头 + 功能码），当前只有 %d 个", len(bytes)),
			fmt.Sprintf("Frame too short: Modbus TCP needs at least 8 bytes (7-byte MBAP header + function code), got %d", len(bytes))))
	}
	tx := u16BE(bytes, 0)
	proto := u16BE(bytes, 2)
	declared := int(u16BE(bytes, 4))
	unit := bytes[6]
	actual := len(bytes) - 6

	rep.pushTech(lang.pick(
		fmt.Sprintf("传输方式: Modbus TCP（共 %d 字节）", len(bytes)),
		fmt.Sprintf("Transport: Modbus TCP (%d bytes total)", len(bytes))))
	rep.pushTech(lang.pick(
		fmt.Sprintf("事务标识符: 0x%04X", tx),
		fmt.Sprintf("Transaction ID: 0x%04X", tx)))
	rep.pushTech(lang.pick(
		fmt.Sprintf("协议标识符: 0x%04X", proto),
		fmt.Sprintf("Protocol ID: 0x%04X", proto)))
	rep.pushTech(lang.pick(
		fmt.Sprintf("长度字段: %d（实际后续 %d 字节）", declared, actual),
		fmt.Sprintf("Length field: %d (%d bytes actually follow)", declared, actual)))
	rep.pushTech(lang.pick(
		fmt.Sprintf("单元标识符: %d", unit),
		fmt.Sprintf("Unit ID: %d", unit)))

	if proto != 0 {
		rep.pushHint(lang.pick(
			fmt.Sprintf("协议标识符是 0x%04X，标准 Modbus TCP 应该是 0x0000", proto),
			fmt.Sprintf("The protocol identifier is 0x%04X; standard Modbus TCP uses 0x0000", proto)))
	}
	if declared != actual {
		if declared > actual {
			rep.pushHint(lang.pick(
				fmt.Sprintf("长度字段是 %d，但实际只收到 %d 个字节，报文可能被截断或长度填错", declared, actual),
				fmt.Sprintf("The length field says %d, but only %d bytes follow; the frame may be truncated or the length is wrong", declared, actual)))
		} else {
			rep.pushHint(lang.pick(
				fmt.Sprintf("长度字段是 %d，但实际收到 %d 个字节，多出的 %d 个可能属于下一条报文", declared, actual, actual-declared),
				fmt.Sprintf("The length field says %d, but %d bytes follow; the extra %d bytes may belong to the next frame", declared, actual, actual-declared)))
		}
	}

	rep.fieldAt(bytes, 0, 2, lang.pick("事务标识符", "Transaction identifier"), lang.pick(
		fmt.Sprintf("主站给本次对话的编号 0x%04X，响应会原样带回", tx),
		fmt.Sprintf("Sequence number 0x%04X assigned by the master to this exchange; the response echoes it", tx)))
	protoDesc := lang.pick("0x0000 = 标准 Modbus 协议", "0x0000 = standard Modbus protocol")
	if proto != 0 {
		protoDesc = lang.pick("标准 Modbus 应为 0x0000", "standard Modbus uses 0x0000")
	}
	rep.fieldAt(bytes, 2, 4, lang.pick("协议标识符", "Protocol identifier"), protoDesc)
	rep.fieldAt(bytes, 4, 6, lang.pick("长度", "Length"), lang.pick(
		fmt.Sprintf("后面还有 %d 个字节（单元标识符 + 数据）%s", declared, lenNote(declared, actual, lang)),
		fmt.Sprintf("%d more bytes follow (unit identifier + data)%s", declared, lenNote(declared, actual, lang))))
	rep.fieldAt(bytes, 6, 7, lang.pick("单元标识符", "Unit identifier"), lang.pick(
		fmt.Sprintf("从站地址：%d 号设备", unit),
		fmt.Sprintf("slave address: device %d", unit)))

	pduEnd := 6 + declared
	if pduEnd < 7 {
		pduEnd = 7
	}
	if pduEnd > len(bytes) {
		pduEnd = len(bytes)
	}
	var extra []byte
	if pduEnd < len(bytes) {
		extra = bytes[pduEnd:]
	}
	return transportResult{raw: bytes, pduOff: 7, pduEnd: pduEnd, unit: unit,
		trailer: trailer{kind: trailerTCP, extra: extra}}, nil
}

func lenNote(declared, actual int, lang Lang) string {
	if declared == actual {
		return ""
	}
	return lang.pick(fmt.Sprintf("，但实际有 %d 个", actual), fmt.Sprintf(", but %d actually follow", actual))
}

func parseRTU(bytes []byte, lang Lang, rep *report) (transportResult, error) {
	if len(bytes) < 4 {
		return transportResult{}, errors.New(lang.pick(
			fmt.Sprintf("报文太短：Modbus RTU 至少需要 4 个字节（地址 + 功能码 + 2 字节 CRC），当前只有 %d 个", len(bytes)),
			fmt.Sprintf("Frame too short: Modbus RTU needs at least 4 bytes (address + function code + 2-byte CRC), got %d", len(bytes))))
	}
	unit := bytes[0]
	crc := tools.CRC16(bytes[:len(bytes)-2])
	lo := byte(crc & 0xFF)
	hi := byte(crc >> 8)
	pass := lo == bytes[len(bytes)-2] && hi == bytes[len(bytes)-1]

	rep.pushTech(lang.pick(
		fmt.Sprintf("传输方式: Modbus RTU（共 %d 字节）", len(bytes)),
		fmt.Sprintf("Transport: Modbus RTU (%d bytes total)", len(bytes))))
	rep.pushTech(lang.pick(
		fmt.Sprintf("从站地址: %d", unit),
		fmt.Sprintf("Slave address: %d", unit)))
	if pass {
		rep.pushHint(lang.pick("CRC 校验通过", "CRC check passed"))
	} else {
		rep.pushHint(lang.pick(
			fmt.Sprintf("CRC 校验失败：报文末尾是 %02X %02X，按前面数据算出来应为 %02X %02X（低字节在前），报文可能有缺漏或被改动",
				bytes[len(bytes)-2], bytes[len(bytes)-1], lo, hi),
			fmt.Sprintf("CRC check failed: the frame ends with %02X %02X, but the data computes to %02X %02X (low byte first); bytes may be missing or altered",
				bytes[len(bytes)-2], bytes[len(bytes)-1], lo, hi)))
	}
	rep.fieldAt(bytes, 0, 1, lang.pick("从站地址", "Slave address"), slaveAddrWord(unit, lang))

	return transportResult{raw: bytes, pduOff: 1, pduEnd: len(bytes) - 2, unit: unit,
		trailer: trailer{kind: trailerRTU, pass: pass, lo: lo, hi: hi, crc: crc}}, nil
}

func parseReadBody(info fcInfo, fcQuoted string, data []byte, isResponse bool, unit byte,
	raw []byte, dataOff int, lang Lang, rep *report) body {
	uw := unitWord(unit, lang)
	if !isResponse {
		if len(data) != 4 {
			rep.pushHint(lang.pick(
				fmt.Sprintf("该功能码的请求数据区应为 4 个字节，实际 %d 个，报文可能不完整", len(data)),
				fmt.Sprintf("The request data area for this function should be 4 bytes, got %d; the frame may be incomplete", len(data))))
		}
		if len(data) >= 4 {
			start := u16BE(data, 0)
			qty := int(u16BE(data, 2))
			plc := plcLabel(info.plcBase, start)
			rep.fieldAt(raw, dataOff, dataOff+2, lang.pick("起始地址", "Start address"), lang.pick(
				fmt.Sprintf("协议地址 %d，对应 PLC 地址 %s", start, plc),
				fmt.Sprintf("protocol address %d, PLC address %s", start, plc)))
			rep.fieldAt(raw, dataOff+2, dataOff+4, lang.pick("读取数量", "Read quantity"), lang.pick(
				fmt.Sprintf("读 %s", qtyUnit(info, qty, lang)),
				fmt.Sprintf("read %s", qtyUnit(info, qty, lang))))
			rep.pushTech(lang.pick(
				fmt.Sprintf("起始地址: %d（PLC %s）", start, plc),
				fmt.Sprintf("Start address: %d (PLC %s)", start, plc)))
			rep.pushTech(lang.pick(fmt.Sprintf("数量: %d", qty), fmt.Sprintf("Quantity: %d", qty)))
			if qty < 1 || qty > int(info.maxQty) {
				rep.pushHint(lang.pick(
					fmt.Sprintf("读取数量 %d 超出协议允许范围（1~%d）", qty, info.maxQty),
					fmt.Sprintf("Read quantity %d is outside the protocol range (1-%d)", qty, info.maxQty)))
			}
			return body{summary: lang.pick(
				fmt.Sprintf("主站向 %s发起%s：从 %s 开始，读 %s", uw, fcQuoted, plc, qtyUnit(info, qty, lang)),
				fmt.Sprintf("The master asks %s to %s: read %s starting from %s", uw, fcQuoted, qtyUnit(info, qty, lang), plc))}
		}
		if len(data) > 0 {
			rep.fieldAt(raw, dataOff, dataOff+len(data), lang.pick("数据区", "Data area"), lang.pick(
				"数据不完整，无法解析起始地址和数量",
				"incomplete data; cannot parse the start address and quantity"))
		}
		return body{summary: lang.pick(
			fmt.Sprintf("主站向 %s发起%s，但数据区不完整（应 4 字节，实际 %d 字节）", uw, fcQuoted, len(data)),
			fmt.Sprintf("The master asks %s to %s, but the data area is incomplete (should be 4 bytes, got %d)", uw, fcQuoted, len(data)))}
	}

	if len(data) == 0 {
		rep.pushHint(lang.pick("响应缺少字节计数和数据", "The response is missing the byte count and data"))
		return body{summary: lang.pick(
			fmt.Sprintf("%s回复%s，但数据区是空的", uw, fcQuoted),
			fmt.Sprintf("%s replied to %s, but the data area is empty", uw, fcQuoted))}
	}
	byteCount := int(data[0])
	payload := data[1:]
	rep.fieldAt(raw, dataOff, dataOff+1, lang.pick("字节计数", "Byte count"), lang.pick(
		fmt.Sprintf("数据区共 %d 个字节", byteCount),
		fmt.Sprintf("%d data bytes", byteCount)))
	rep.pushTech(lang.pick(
		fmt.Sprintf("字节计数: %d", byteCount),
		fmt.Sprintf("Byte count: %d", byteCount)))
	if byteCount != len(payload) {
		rep.pushHint(lang.pick(
			fmt.Sprintf("字节计数说数据区有 %d 个字节，实际跟着 %d 个，报文可能被截断或有多余字节", byteCount, len(payload)),
			fmt.Sprintf("The byte count says %d data bytes, but %d follow; the frame may be truncated or have extra bytes", byteCount, len(payload))))
	}
	usable := payload[:usableLen(byteCount, len(payload))]
	if len(usable) == 0 {
		rep.pushHint(lang.pick("字节计数为 0，从站没有返回任何数据", "The byte count is 0; the slave returned no data"))
		return body{summary: lang.pick(
			fmt.Sprintf("%s回复%s，但没有返回任何数据", uw, fcQuoted),
			fmt.Sprintf("%s replied to %s, but returned no data", uw, fcQuoted))}
	}
	rep.fieldAt(raw, dataOff+1, dataOff+1+len(usable), lang.pick("数据", "Data"),
		dataAreaDesc(info, lang))
	rep.pushTech(lang.pick(
		fmt.Sprintf("数据: %s", tools.FormatHex(usable, " ")),
		fmt.Sprintf("Data: %s", tools.FormatHex(usable, " "))))

	if info.kind == kindBits {
		rows := make([]string, 0, len(usable)*8)
		on := 0
		for i, b := range usable {
			for bit := 0; bit < 8; bit++ {
				isOn := (b>>uint(bit))&1 == 1
				if isOn {
					on++
				}
				rows = append(rows, fmt.Sprintf("%s = %s",
					plcLabel(info.plcBase, uint16(i*8+bit)), onOff(isOn, lang)))
			}
		}
		total := len(rows)
		note := lang.pick(
			fmt.Sprintf("注：响应报文本身不含起始地址和数量，编号默认从 %s 起；最后一个字节可能带填充位，实际以对应请求为准", plcLabel(info.plcBase, 0)),
			fmt.Sprintf("Note: the response frame carries no start address or quantity; numbering starts at %s by default and the last byte may contain padding bits — refer to the matching request", plcLabel(info.plcBase, 0)))
		return body{
			summary: lang.pick(
				fmt.Sprintf("%s回复%s：共 %d 位状态，ON %d 个 / OFF %d 个（明细见下）", uw, fcQuoted, total, on, total-on),
				fmt.Sprintf("%s replied to %s: %d bit states, %d ON / %d OFF (details below)", uw, fcQuoted, total, on, total-on)),
			detailRows: rows,
			detailNote: note,
		}
	}

	if byteCount%2 != 0 {
		rep.pushHint(lang.pick(
			fmt.Sprintf("寄存器值应为偶数个字节，字节计数 %d 是奇数，报文可能有误", byteCount),
			fmt.Sprintf("Register values should be an even number of bytes, but the byte count %d is odd; the frame may be malformed", byteCount)))
	}
	rows := registerRows(info, usable, 0, lang)
	note := lang.pick(
		fmt.Sprintf("注：响应报文本身不含起始地址，编号默认从 %s 起，实际起始地址以对应请求为准", plcLabel(info.plcBase, 0)),
		fmt.Sprintf("Note: the response frame carries no start address; numbering starts at %s by default — the real start address comes from the matching request", plcLabel(info.plcBase, 0)))
	summary := lang.pick(
		fmt.Sprintf("%s回复%s：%s", uw, fcQuoted, rows[0]),
		fmt.Sprintf("%s replied to %s: %s", uw, fcQuoted, rows[0]))
	if len(rows) > 1 {
		summary = lang.pick(
			fmt.Sprintf("%s回复%s：共 %d 个寄存器，从 %s 开始（明细见下）", uw, fcQuoted, len(rows), plcLabel(info.plcBase, 0)),
			fmt.Sprintf("%s replied to %s: %d registers starting from %s (details below)", uw, fcQuoted, len(rows), plcLabel(info.plcBase, 0)))
	}
	return body{summary: summary, detailRows: rows, detailNote: note}
}

// dataAreaDesc explains how to read the payload of a read response.
func dataAreaDesc(info fcInfo, lang Lang) string {
	if info.kind == kindBits {
		return lang.pick(
			fmt.Sprintf("每一位代表一个%s的状态（每个字节内低位在前）", info.unitZh),
			fmt.Sprintf("each bit is the state of one %s (LSB first within each byte)", info.unitEn))
	}
	return lang.pick("每 2 个字节是一个寄存器的值", "every 2 bytes are one register value")
}

func onOff(on bool, lang Lang) string {
	if on {
		return "ON"
	}
	return "OFF"
}

// registerRows renders one line per 16-bit value, dropping a trailing odd byte
// like Rust's chunks_exact(2).
func registerRows(info fcInfo, usable []byte, start uint16, lang Lang) []string {
	rows := make([]string, 0, len(usable)/2)
	for i := 0; i+1 < len(usable); i += 2 {
		value := u16BE(usable, i)
		rows = append(rows, lang.pick(
			fmt.Sprintf("%s = %d（0x%04X）", plcLabel(info.plcBase, start+uint16(i/2)), value, value),
			fmt.Sprintf("%s = %d (0x%04X)", plcLabel(info.plcBase, start+uint16(i/2)), value, value)))
	}
	return rows
}

func parseWriteSingleBody(info fcInfo, fcQuoted string, data []byte, isResponse bool, unit byte,
	raw []byte, dataOff int, lang Lang, rep *report) body {
	uw := unitWord(unit, lang)
	if len(data) != 4 {
		rep.pushHint(lang.pick(
			fmt.Sprintf("该功能码的数据区应为 4 个字节，实际 %d 个，报文可能不完整", len(data)),
			fmt.Sprintf("The data area for this function should be 4 bytes, got %d; the frame may be incomplete", len(data))))
	}
	if len(data) < 4 {
		if len(data) > 0 {
			rep.fieldAt(raw, dataOff, dataOff+len(data), lang.pick("数据区", "Data area"),
				lang.pick("数据不完整", "incomplete data"))
		}
		head := lang.pick(
			fmt.Sprintf("主站向 %s发起%s", uw, fcQuoted),
			fmt.Sprintf("The master asks %s to %s", uw, fcQuoted))
		if isResponse {
			head = lang.pick(
				fmt.Sprintf("%s回复%s", uw, fcQuoted),
				fmt.Sprintf("%s replied to %s", uw, fcQuoted))
		}
		return body{summary: lang.pick(
			fmt.Sprintf("%s，但数据区不完整", head),
			fmt.Sprintf("%s, but the data area is incomplete", head))}
	}

	addr := u16BE(data, 0)
	val := u16BE(data, 2)
	plc := plcLabel(info.plcBase, addr)
	rep.fieldAt(raw, dataOff, dataOff+2, lang.pick("地址", "Address"), lang.pick(
		fmt.Sprintf("协议地址 %d，对应 PLC 地址 %s", addr, plc),
		fmt.Sprintf("protocol address %d, PLC address %s", addr, plc)))
	rep.pushTech(lang.pick(
		fmt.Sprintf("地址: %d（PLC %s）", addr, plc),
		fmt.Sprintf("Address: %d (PLC %s)", addr, plc)))

	if info.kind != kindWriteSingleCoil {
		rep.fieldAt(raw, dataOff+2, dataOff+4, lang.pick("写入值", "Write value"), lang.pick(
			fmt.Sprintf("十进制 %d，十六进制 0x%04X", val, val),
			fmt.Sprintf("decimal %d, hexadecimal 0x%04X", val, val)))
		rep.pushTech(lang.pick(
			fmt.Sprintf("写入值: %d（0x%04X）", val, val),
			fmt.Sprintf("Write value: %d (0x%04X)", val, val)))
		row := lang.pick(
			fmt.Sprintf("%s = %d（0x%04X）", plc, val, val),
			fmt.Sprintf("%s = %d (0x%04X)", plc, val, val))
		summary := lang.pick(
			fmt.Sprintf("主站向 %s发起%s：把寄存器 %s 写为 %d（0x%04X）", uw, fcQuoted, plc, val, val),
			fmt.Sprintf("The master asks %s to %s: set register %s to %d (0x%04X)", uw, fcQuoted, plc, val, val))
		if isResponse {
			summary = lang.pick(
				fmt.Sprintf("%s确认：寄存器 %s 已写为 %d（0x%04X）", uw, plc, val, val),
				fmt.Sprintf("%s confirmed: register %s is now %d (0x%04X)", uw, plc, val, val))
		}
		return body{summary: summary, detailRows: []string{row}}
	}

	onoff := "ON"
	if val == 0x0000 {
		onoff = "OFF"
	} else if val != 0xFF00 {
		rep.pushHint(lang.pick(
			fmt.Sprintf("写线圈的值应为 0xFF00（ON）或 0x0000（OFF），实际是 0x%04X", val),
			fmt.Sprintf("A coil write value should be 0xFF00 (ON) or 0x0000 (OFF), got 0x%04X", val)))
		onoff = lang.pick("未知", "an unknown value")
	}
	valueDesc := lang.pick(
		fmt.Sprintf("0x%04X，不是标准值（0xFF00=ON，0x0000=OFF）", val),
		fmt.Sprintf("0x%04X, not a standard value (0xFF00=ON, 0x0000=OFF)", val))
	if val == 0xFF00 || val == 0x0000 {
		valueDesc = fmt.Sprintf("0x%04X = %s", val, onoff)
	}
	rep.fieldAt(raw, dataOff+2, dataOff+4, lang.pick("写入值", "Write value"), valueDesc)
	rep.pushTech(lang.pick(
		fmt.Sprintf("写入值: 0x%04X（%s）", val, onoff),
		fmt.Sprintf("Write value: 0x%04X (%s)", val, onoff)))

	onOffText := "OFF"
	if val == 0xFF00 {
		onOffText = "ON"
	}
	coilSummary := lang.pick(
		fmt.Sprintf("主站向 %s发起%s：把线圈 %s 设为 %s", uw, fcQuoted, plc, onoff),
		fmt.Sprintf("The master asks %s to %s: set coil %s to %s", uw, fcQuoted, plc, onoff))
	if isResponse {
		coilSummary = lang.pick(
			fmt.Sprintf("%s确认：线圈 %s 已设为 %s", uw, plc, onoff),
			fmt.Sprintf("%s confirmed: coil %s is now %s", uw, plc, onoff))
	}
	return body{
		summary:    coilSummary,
		detailRows: []string{fmt.Sprintf("%s = %s", plc, onOffText)},
	}
}

func parseWriteMultiBody(info fcInfo, fcQuoted string, data []byte, isResponse bool, unit byte,
	raw []byte, dataOff int, lang Lang, rep *report) body {
	uw := unitWord(unit, lang)
	if isResponse {
		if len(data) != 4 {
			rep.pushHint(lang.pick(
				fmt.Sprintf("该功能码的响应数据区应为 4 个字节，实际 %d 个", len(data)),
				fmt.Sprintf("The response data area for this function should be 4 bytes, got %d", len(data))))
		}
		if len(data) < 4 {
			if len(data) > 0 {
				rep.fieldAt(raw, dataOff, dataOff+len(data), lang.pick("数据区", "Data area"),
					lang.pick("数据不完整", "incomplete data"))
			}
			return body{summary: lang.pick(
				fmt.Sprintf("%s回复%s，但数据区不完整", uw, fcQuoted),
				fmt.Sprintf("%s replied to %s, but the data area is incomplete", uw, fcQuoted))}
		}
		start := u16BE(data, 0)
		qty := int(u16BE(data, 2))
		plc := plcLabel(info.plcBase, start)
		rep.fieldAt(raw, dataOff, dataOff+2, lang.pick("起始地址", "Start address"), lang.pick(
			fmt.Sprintf("协议地址 %d，对应 PLC 地址 %s", start, plc),
			fmt.Sprintf("protocol address %d, PLC address %s", start, plc)))
		rep.fieldAt(raw, dataOff+2, dataOff+4, lang.pick("写入数量", "Write quantity"), lang.pick(
			fmt.Sprintf("成功写入 %s", qtyUnit(info, qty, lang)),
			fmt.Sprintf("successfully wrote %s", qtyUnit(info, qty, lang))))
		rep.pushTech(lang.pick(
			fmt.Sprintf("起始地址: %d（PLC %s）", start, plc),
			fmt.Sprintf("Start address: %d (PLC %s)", start, plc)))
		rep.pushTech(lang.pick(
			fmt.Sprintf("写入数量: %d", qty),
			fmt.Sprintf("Write quantity: %d", qty)))
		return body{summary: lang.pick(
			fmt.Sprintf("%s确认：已从 %s 开始写入 %s", uw, plc, qtyUnit(info, qty, lang)),
			fmt.Sprintf("%s confirmed: wrote %s starting from %s", uw, qtyUnit(info, qty, lang), plc))}
	}

	if len(data) < 5 {
		rep.pushHint(lang.pick(
			fmt.Sprintf("该功能码的请求数据区至少 5 个字节，实际 %d 个，报文不完整", len(data)),
			fmt.Sprintf("The request data area for this function needs at least 5 bytes, got %d; the frame is incomplete", len(data))))
		if len(data) > 0 {
			rep.fieldAt(raw, dataOff, dataOff+len(data), lang.pick("数据区", "Data area"),
				lang.pick("数据不完整", "incomplete data"))
		}
		return body{summary: lang.pick(
			fmt.Sprintf("主站向 %s发起%s，但数据区不完整", uw, fcQuoted),
			fmt.Sprintf("The master asks %s to %s, but the data area is incomplete", uw, fcQuoted))}
	}

	start := u16BE(data, 0)
	qty := int(u16BE(data, 2))
	byteCount := int(data[4])
	payload := data[5:]
	plc := plcLabel(info.plcBase, start)
	rep.fieldAt(raw, dataOff, dataOff+2, lang.pick("起始地址", "Start address"), lang.pick(
		fmt.Sprintf("协议地址 %d，对应 PLC 地址 %s", start, plc),
		fmt.Sprintf("protocol address %d, PLC address %s", start, plc)))
	rep.fieldAt(raw, dataOff+2, dataOff+4, lang.pick("写入数量", "Write quantity"), lang.pick(
		fmt.Sprintf("写 %s", qtyUnit(info, qty, lang)),
		fmt.Sprintf("write %s", qtyUnit(info, qty, lang))))
	rep.fieldAt(raw, dataOff+4, dataOff+5, lang.pick("字节计数", "Byte count"), lang.pick(
		fmt.Sprintf("写入数据共 %d 个字节", byteCount),
		fmt.Sprintf("%d bytes of write data", byteCount)))
	rep.pushTech(lang.pick(
		fmt.Sprintf("起始地址: %d（PLC %s）", start, plc),
		fmt.Sprintf("Start address: %d (PLC %s)", start, plc)))
	rep.pushTech(lang.pick(
		fmt.Sprintf("写入数量: %d", qty),
		fmt.Sprintf("Write quantity: %d", qty)))
	rep.pushTech(lang.pick(
		fmt.Sprintf("字节计数: %d", byteCount),
		fmt.Sprintf("Byte count: %d", byteCount)))

	if qty < 1 || qty > int(info.maxQty) {
		rep.pushHint(lang.pick(
			fmt.Sprintf("写入数量 %d 超出协议允许范围（1~%d）", qty, info.maxQty),
			fmt.Sprintf("Write quantity %d is outside the protocol range (1-%d)", qty, info.maxQty)))
	}
	expected := qty * 2
	if info.kind == kindWriteMultiCoil {
		expected = (qty + 7) / 8
	}
	if byteCount != expected {
		rep.pushHint(lang.pick(
			fmt.Sprintf("要写 %s，数据应是 %d 个字节，字节计数却写 %d", qtyUnit(info, qty, lang), expected, byteCount),
			fmt.Sprintf("Writing %s needs %d data bytes, but the byte count says %d", qtyUnit(info, qty, lang), expected, byteCount)))
	}
	if byteCount != len(payload) {
		rep.pushHint(lang.pick(
			fmt.Sprintf("字节计数说后面有 %d 个字节，实际跟着 %d 个", byteCount, len(payload)),
			fmt.Sprintf("The byte count says %d bytes follow, but %d actually follow", byteCount, len(payload))))
	}
	usable := payload[:usableLen(byteCount, len(payload))]
	if len(usable) > 0 {
		rep.fieldAt(raw, dataOff+5, dataOff+5+len(usable), lang.pick("写入数据", "Write data"),
			writeDataDesc(info, lang))
		rep.pushTech(lang.pick(
			fmt.Sprintf("数据: %s", tools.FormatHex(usable, " ")),
			fmt.Sprintf("Data: %s", tools.FormatHex(usable, " "))))
	}

	if info.kind == kindWriteMultiCoil {
		rows := make([]string, 0, qty)
		on := 0
	outer:
		for i, b := range usable {
			for bit := 0; bit < 8; bit++ {
				if len(rows) >= qty {
					break outer
				}
				isOn := (b>>uint(bit))&1 == 1
				if isOn {
					on++
				}
				idx := int(start) + i*8 + bit
				rows = append(rows, fmt.Sprintf("%s = %s",
					plcLabel(info.plcBase, uint16(idx)), onOff(isOn, lang)))
			}
		}
		return body{summary: lang.pick(
			fmt.Sprintf("主站向 %s发起%s：从 %s 开始写 %s（ON %d 个 / OFF %d 个）", uw, fcQuoted, plc, qtyUnit(info, qty, lang), on, len(rows)-on),
			fmt.Sprintf("The master asks %s to %s: write %s starting from %s (%d ON / %d OFF)", uw, fcQuoted, qtyUnit(info, qty, lang), plc, on, len(rows)-on)),
			detailRows: rows}
	}

	rows := make([]string, 0, qty)
	for i := 0; i+1 < len(usable) && len(rows) < qty; i += 2 {
		value := u16BE(usable, i)
		idx := int(start) + len(rows)
		rows = append(rows, lang.pick(
			fmt.Sprintf("%s = %d（0x%04X）", plcLabel(info.plcBase, uint16(idx)), value, value),
			fmt.Sprintf("%s = %d (0x%04X)", plcLabel(info.plcBase, uint16(idx)), value, value)))
	}
	return body{summary: lang.pick(
		fmt.Sprintf("主站向 %s发起%s：从 %s 开始写 %s", uw, fcQuoted, plc, qtyUnit(info, qty, lang)),
		fmt.Sprintf("The master asks %s to %s: write %s starting from %s", uw, fcQuoted, qtyUnit(info, qty, lang), plc)),
		detailRows: rows}
}

func writeDataDesc(info fcInfo, lang Lang) string {
	if info.kind == kindWriteMultiCoil {
		return lang.pick(
			"每一位代表一个线圈要写的状态（每个字节内低位在前）",
			"each bit is the state to write to one coil (LSB first within each byte)")
	}
	return lang.pick("每 2 个字节是一个寄存器要写的值", "every 2 bytes are one register value to write")
}
