use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use thiserror::Error;

/// Modbus register types (four areas)
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum RegisterType {
    /// 0x - read/write single bit
    Coil,
    /// 1x - read-only single bit
    DiscreteInput,
    /// 3x - read-only 16-bit
    InputRegister,
    /// 4x - read/write 16-bit
    HoldingRegister,
}

/// Data types for interpreting register values
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum DataType {
    Bool,
    #[serde(alias = "uint16")]
    UInt16,
    Int16,
    #[serde(alias = "uint32")]
    UInt32,
    Int32,
    Float32,
}

impl DataType {
    /// Number of 16-bit registers this data type occupies
    pub fn register_count(&self) -> u16 {
        match self {
            DataType::Bool | DataType::UInt16 | DataType::Int16 => 1,
            DataType::UInt32 | DataType::Int32 | DataType::Float32 => 2,
        }
    }
}

/// Byte order for multi-register data types
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
#[derive(Default)]
pub enum Endian {
    /// AB CD (most common)
    #[default]
    Big,
    /// CD AB
    Little,
    /// BA DC
    MidBig,
    /// DC BA
    MidLittle,
}

/// Metadata definition for a register (used for UI display and config export)
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RegisterDef {
    pub address: u16,
    pub register_type: RegisterType,
    pub data_type: DataType,
    #[serde(default)]
    pub endian: Endian,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub comment: String,
    /// Per-point random mutation config; `None` = not configured. Persisted.
    #[serde(default)]
    pub mutation: Option<crate::mutation::MutationConfig>,
    /// Optional generated-value source for this point. Persisted.
    #[serde(default)]
    pub data_source: Option<crate::data_source::DataSourceConfig>,
}

/// Inclusive address range occupied by a point definition. Bit areas always
/// occupy one address; 32-bit register values occupy two consecutive words.
/// Returns `None` when the range would overflow address 65535.
pub fn occupied_address_range(def: &RegisterDef) -> Option<(u16, u16)> {
    let count = match def.register_type {
        RegisterType::Coil | RegisterType::DiscreteInput => 1,
        RegisterType::HoldingRegister | RegisterType::InputRegister => {
            def.data_type.register_count()
        }
    };
    let end = def.address.checked_add(count.saturating_sub(1))?;
    Some((def.address, end))
}

/// Whether two point definitions ambiguously claim at least one address in
/// the same Modbus data area. Overflowing definitions are treated as invalid.
pub fn register_definitions_overlap(left: &RegisterDef, right: &RegisterDef) -> bool {
    if left.register_type != right.register_type {
        return false;
    }
    let (Some((left_start, left_end)), Some((right_start, right_end))) =
        (occupied_address_range(left), occupied_address_range(right))
    else {
        return true;
    };
    left_start <= right_end && right_start <= left_end
}

#[derive(Debug, Error, PartialEq, Eq)]
pub enum RegisterDefinitionError {
    #[error("register {register_type:?}@{address} exceeds address 65535")]
    AddressOverflow {
        register_type: RegisterType,
        address: u16,
    },
    #[error("register {register_type:?}@{address} overlaps point at address {other_address}")]
    Overlap {
        register_type: RegisterType,
        address: u16,
        other_address: u16,
    },
}

/// Validate a complete point-definition set in O(n log n) time.
///
/// Definitions are sorted by Modbus area and occupied address, so only
/// adjacent ranges within an area need to be compared for overlap.
pub fn validate_register_definitions(
    definitions: &[RegisterDef],
) -> Result<(), RegisterDefinitionError> {
    let mut ranges = Vec::with_capacity(definitions.len());
    for definition in definitions {
        let Some((start, end)) = occupied_address_range(definition) else {
            return Err(RegisterDefinitionError::AddressOverflow {
                register_type: definition.register_type,
                address: definition.address,
            });
        };
        ranges.push((definition.register_type, start, end));
    }
    ranges.sort_unstable();

    for pair in ranges.windows(2) {
        let (register_type, start, end) = pair[0];
        let (other_type, other_start, _) = pair[1];
        if register_type == other_type && other_start <= end {
            return Err(RegisterDefinitionError::Overlap {
                register_type,
                address: start,
                other_address: other_start,
            });
        }
    }
    Ok(())
}

#[derive(Debug, Error)]
pub enum RegisterError {
    #[error("value {0} out of range for {1:?}")]
    ValueOutOfRange(f64, DataType),
    #[error("address {0} not found")]
    AddressNotFound(u16),
    #[error("invalid data for conversion")]
    InvalidData,
}

/// Storage for the four Modbus register areas
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct RegisterMap {
    pub coils: HashMap<u16, bool>,
    pub discrete_inputs: HashMap<u16, bool>,
    pub holding_registers: HashMap<u16, u16>,
    pub input_registers: HashMap<u16, u16>,
}

impl RegisterMap {
    pub fn new() -> Self {
        Self::default()
    }

    // --- Coil operations ---

    pub fn read_coils(&self, start: u16, count: u16) -> Vec<bool> {
        (start..start + count)
            .map(|addr| self.coils.get(&addr).copied().unwrap_or(false))
            .collect()
    }

    pub fn write_coil(&mut self, addr: u16, value: bool) {
        self.coils.insert(addr, value);
    }

    pub fn write_coils(&mut self, start: u16, values: &[bool]) {
        for (i, &val) in values.iter().enumerate() {
            self.coils.insert(start + i as u16, val);
        }
    }

    // --- Discrete input operations ---

    pub fn read_discrete_inputs(&self, start: u16, count: u16) -> Vec<bool> {
        (start..start + count)
            .map(|addr| self.discrete_inputs.get(&addr).copied().unwrap_or(false))
            .collect()
    }

    // --- Holding register operations ---

    pub fn read_holding_registers(&self, start: u16, count: u16) -> Vec<u16> {
        (start..start + count)
            .map(|addr| self.holding_registers.get(&addr).copied().unwrap_or(0))
            .collect()
    }

    pub fn write_holding_register(&mut self, addr: u16, value: u16) {
        self.holding_registers.insert(addr, value);
    }

    pub fn write_holding_registers(&mut self, start: u16, values: &[u16]) {
        for (i, &val) in values.iter().enumerate() {
            self.holding_registers.insert(start + i as u16, val);
        }
    }

    // --- Input register operations ---

    pub fn read_input_registers(&self, start: u16, count: u16) -> Vec<u16> {
        (start..start + count)
            .map(|addr| self.input_registers.get(&addr).copied().unwrap_or(0))
            .collect()
    }

    // --- Address validation ---

    pub fn has_all_coils(&self, start: u16, count: u16) -> bool {
        (0..count).all(|offset| {
            start
                .checked_add(offset)
                .is_some_and(|address| self.coils.contains_key(&address))
        })
    }

    pub fn has_all_discrete_inputs(&self, start: u16, count: u16) -> bool {
        (0..count).all(|offset| {
            start
                .checked_add(offset)
                .is_some_and(|address| self.discrete_inputs.contains_key(&address))
        })
    }

    pub fn has_all_holding_registers(&self, start: u16, count: u16) -> bool {
        (0..count).all(|offset| {
            start
                .checked_add(offset)
                .is_some_and(|address| self.holding_registers.contains_key(&address))
        })
    }

    pub fn has_all_input_registers(&self, start: u16, count: u16) -> bool {
        (0..count).all(|offset| {
            start
                .checked_add(offset)
                .is_some_and(|address| self.input_registers.contains_key(&address))
        })
    }

    pub fn has_coil(&self, addr: u16) -> bool {
        self.coils.contains_key(&addr)
    }
    pub fn has_holding_register(&self, addr: u16) -> bool {
        self.holding_registers.contains_key(&addr)
    }

    /// Ensure map entries exist for every address covered by `def`.
    /// Uses default values (0 / false) only for addresses that are absent,
    /// so existing register values are preserved when the def is re-added.
    pub fn ensure_from_def(&mut self, def: &RegisterDef) {
        let count = match def.register_type {
            RegisterType::Coil | RegisterType::DiscreteInput => 1,
            RegisterType::HoldingRegister | RegisterType::InputRegister => {
                def.data_type.register_count()
            }
        };
        for i in 0..count {
            let Some(addr) = def.address.checked_add(i) else {
                break;
            };
            match def.register_type {
                RegisterType::Coil => {
                    self.coils.entry(addr).or_insert(false);
                }
                RegisterType::DiscreteInput => {
                    self.discrete_inputs.entry(addr).or_insert(false);
                }
                RegisterType::HoldingRegister => {
                    self.holding_registers.entry(addr).or_insert(0);
                }
                RegisterType::InputRegister => {
                    self.input_registers.entry(addr).or_insert(0);
                }
            }
        }
    }

    /// Remove every raw address occupied by a point definition.
    pub fn remove_from_def(&mut self, def: &RegisterDef) {
        let Some((start, end)) = occupied_address_range(def) else {
            return;
        };
        for address in start..=end {
            match def.register_type {
                RegisterType::Coil => {
                    self.coils.remove(&address);
                }
                RegisterType::DiscreteInput => {
                    self.discrete_inputs.remove(&address);
                }
                RegisterType::HoldingRegister => {
                    self.holding_registers.remove(&address);
                }
                RegisterType::InputRegister => {
                    self.input_registers.remove(&address);
                }
            }
        }
    }

    /// Replace a point's occupied raw range while preserving as many raw
    /// values as possible when the Modbus area is unchanged.
    pub fn replace_def(&mut self, original: &RegisterDef, replacement: &RegisterDef) {
        let old_range = occupied_address_range(original);
        let bool_values = if original.register_type == replacement.register_type
            && matches!(
                original.register_type,
                RegisterType::Coil | RegisterType::DiscreteInput
            ) {
            old_range.map(|(start, end)| {
                (start..=end)
                    .map(|address| match original.register_type {
                        RegisterType::Coil => self.coils.get(&address).copied().unwrap_or(false),
                        RegisterType::DiscreteInput => {
                            self.discrete_inputs.get(&address).copied().unwrap_or(false)
                        }
                        _ => unreachable!(),
                    })
                    .collect::<Vec<_>>()
            })
        } else {
            None
        };
        let word_values = if original.register_type == replacement.register_type
            && matches!(
                original.register_type,
                RegisterType::HoldingRegister | RegisterType::InputRegister
            ) {
            old_range.map(|(start, end)| {
                (start..=end)
                    .map(|address| match original.register_type {
                        RegisterType::HoldingRegister => {
                            self.holding_registers.get(&address).copied().unwrap_or(0)
                        }
                        RegisterType::InputRegister => {
                            self.input_registers.get(&address).copied().unwrap_or(0)
                        }
                        _ => unreachable!(),
                    })
                    .collect::<Vec<_>>()
            })
        } else {
            None
        };

        self.remove_from_def(original);
        self.ensure_from_def(replacement);

        if let Some(values) = bool_values {
            for (offset, value) in values.into_iter().enumerate() {
                let Some(address) = replacement.address.checked_add(offset as u16) else {
                    break;
                };
                match replacement.register_type {
                    RegisterType::Coil => {
                        if let Some(slot) = self.coils.get_mut(&address) {
                            *slot = value;
                        }
                    }
                    RegisterType::DiscreteInput => {
                        if let Some(slot) = self.discrete_inputs.get_mut(&address) {
                            *slot = value;
                        }
                    }
                    _ => unreachable!(),
                }
            }
        }
        if let Some(values) = word_values {
            let replacement_count = replacement.data_type.register_count() as usize;
            for (offset, value) in values.into_iter().take(replacement_count).enumerate() {
                let Some(address) = replacement.address.checked_add(offset as u16) else {
                    break;
                };
                match replacement.register_type {
                    RegisterType::HoldingRegister => {
                        if let Some(slot) = self.holding_registers.get_mut(&address) {
                            *slot = value;
                        }
                    }
                    RegisterType::InputRegister => {
                        if let Some(slot) = self.input_registers.get_mut(&address) {
                            *slot = value;
                        }
                    }
                    _ => unreachable!(),
                }
            }
        }
    }
}

// --- Data type encoding/decoding with endian support ---

/// Encode a typed value into one or two raw u16 registers
pub fn encode_value(
    value: f64,
    data_type: DataType,
    endian: Endian,
) -> Result<Vec<u16>, RegisterError> {
    validate_range(value, data_type)?;
    match data_type {
        DataType::Bool => Ok(vec![if value != 0.0 { 1 } else { 0 }]),
        DataType::UInt16 => Ok(vec![value as u16]),
        DataType::Int16 => Ok(vec![(value as i16) as u16]),
        DataType::UInt32 => {
            let raw = (value as u32).to_be_bytes();
            Ok(apply_endian_encode(raw, endian))
        }
        DataType::Int32 => {
            let raw = (value as i32).to_be_bytes();
            Ok(apply_endian_encode(raw, endian))
        }
        DataType::Float32 => {
            let raw = (value as f32).to_be_bytes();
            Ok(apply_endian_encode(raw, endian))
        }
    }
}

/// Decode one or two raw u16 registers into a typed value
pub fn decode_value(
    registers: &[u16],
    data_type: DataType,
    endian: Endian,
) -> Result<f64, RegisterError> {
    match data_type {
        DataType::Bool => {
            let v = registers.first().ok_or(RegisterError::InvalidData)?;
            Ok(if *v != 0 { 1.0 } else { 0.0 })
        }
        DataType::UInt16 => {
            let v = registers.first().ok_or(RegisterError::InvalidData)?;
            Ok(*v as f64)
        }
        DataType::Int16 => {
            let v = registers.first().ok_or(RegisterError::InvalidData)?;
            Ok((*v as i16) as f64)
        }
        DataType::UInt32 => {
            if registers.len() < 2 {
                return Err(RegisterError::InvalidData);
            }
            let bytes = apply_endian_decode(registers[0], registers[1], endian);
            Ok(u32::from_be_bytes(bytes) as f64)
        }
        DataType::Int32 => {
            if registers.len() < 2 {
                return Err(RegisterError::InvalidData);
            }
            let bytes = apply_endian_decode(registers[0], registers[1], endian);
            Ok(i32::from_be_bytes(bytes) as f64)
        }
        DataType::Float32 => {
            if registers.len() < 2 {
                return Err(RegisterError::InvalidData);
            }
            let bytes = apply_endian_decode(registers[0], registers[1], endian);
            Ok(f32::from_be_bytes(bytes) as f64)
        }
    }
}

/// Validate that a value is within the range of the given data type
pub fn validate_range(value: f64, data_type: DataType) -> Result<(), RegisterError> {
    let valid = match data_type {
        DataType::Bool => value == 0.0 || value == 1.0,
        DataType::UInt16 => value >= 0.0 && value <= u16::MAX as f64 && value.fract() == 0.0,
        DataType::Int16 => {
            value >= i16::MIN as f64 && value <= i16::MAX as f64 && value.fract() == 0.0
        }
        DataType::UInt32 => value >= 0.0 && value <= u32::MAX as f64 && value.fract() == 0.0,
        DataType::Int32 => {
            value >= i32::MIN as f64 && value <= i32::MAX as f64 && value.fract() == 0.0
        }
        DataType::Float32 => true, // any f64 that can be cast to f32
    };
    if valid {
        Ok(())
    } else {
        Err(RegisterError::ValueOutOfRange(value, data_type))
    }
}

// Encode 4 bytes (big-endian) into two u16 registers with endian transformation
fn apply_endian_encode(be_bytes: [u8; 4], endian: Endian) -> Vec<u16> {
    let [a, b, c, d] = be_bytes;
    match endian {
        Endian::Big => {
            // AB CD
            vec![u16::from_be_bytes([a, b]), u16::from_be_bytes([c, d])]
        }
        Endian::Little => {
            // CD AB
            vec![u16::from_be_bytes([c, d]), u16::from_be_bytes([a, b])]
        }
        Endian::MidBig => {
            // BA DC
            vec![u16::from_be_bytes([b, a]), u16::from_be_bytes([d, c])]
        }
        Endian::MidLittle => {
            // DC BA
            vec![u16::from_be_bytes([d, c]), u16::from_be_bytes([b, a])]
        }
    }
}

// Decode two u16 registers into 4 big-endian bytes with endian transformation
fn apply_endian_decode(reg0: u16, reg1: u16, endian: Endian) -> [u8; 4] {
    let r0 = reg0.to_be_bytes();
    let r1 = reg1.to_be_bytes();
    match endian {
        Endian::Big => {
            // reg0=AB, reg1=CD → ABCD
            [r0[0], r0[1], r1[0], r1[1]]
        }
        Endian::Little => {
            // reg0=CD, reg1=AB → ABCD
            [r1[0], r1[1], r0[0], r0[1]]
        }
        Endian::MidBig => {
            // reg0=BA, reg1=DC → ABCD
            [r0[1], r0[0], r1[1], r1[0]]
        }
        Endian::MidLittle => {
            // reg0=DC, reg1=BA → ABCD
            [r1[1], r1[0], r0[1], r0[0]]
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_register_type_serde() {
        let json = serde_json::to_string(&RegisterType::HoldingRegister).unwrap();
        assert_eq!(json, "\"holding_register\"");
    }

    #[test]
    fn test_data_type_register_count() {
        assert_eq!(DataType::Bool.register_count(), 1);
        assert_eq!(DataType::UInt16.register_count(), 1);
        assert_eq!(DataType::Float32.register_count(), 2);
        assert_eq!(DataType::UInt32.register_count(), 2);
    }

    #[test]
    fn multi_word_definition_detects_overlap_and_overflow() {
        let make = |address, data_type| RegisterDef {
            address,
            register_type: RegisterType::HoldingRegister,
            data_type,
            endian: Endian::Big,
            name: String::new(),
            comment: String::new(),
            mutation: None,
            data_source: None,
        };
        let wide = make(100, DataType::Float32);
        assert_eq!(occupied_address_range(&wide), Some((100, 101)));
        assert!(register_definitions_overlap(
            &wide,
            &make(101, DataType::UInt16)
        ));
        assert!(!register_definitions_overlap(
            &wide,
            &make(102, DataType::UInt16)
        ));
        assert_eq!(
            occupied_address_range(&make(u16::MAX, DataType::UInt32)),
            None
        );
    }

    #[test]
    fn validates_eighty_thousand_points_without_quadratic_scan() {
        let mut definitions = Vec::with_capacity(80_004);
        for address in 0..=20_000 {
            for register_type in [
                RegisterType::Coil,
                RegisterType::DiscreteInput,
                RegisterType::HoldingRegister,
                RegisterType::InputRegister,
            ] {
                definitions.push(RegisterDef {
                    address,
                    register_type,
                    data_type: if matches!(
                        register_type,
                        RegisterType::Coil | RegisterType::DiscreteInput
                    ) {
                        DataType::Bool
                    } else {
                        DataType::UInt16
                    },
                    endian: Endian::Big,
                    name: String::new(),
                    comment: String::new(),
                    mutation: None,
                    data_source: None,
                });
            }
        }
        assert_eq!(definitions.len(), 80_004);
        assert!(validate_register_definitions(&definitions).is_ok());
    }

    // --- Encode/Decode round-trip tests ---

    #[test]
    fn test_uint16_round_trip() {
        let regs = encode_value(1234.0, DataType::UInt16, Endian::Big).unwrap();
        assert_eq!(regs, vec![1234]);
        let val = decode_value(&regs, DataType::UInt16, Endian::Big).unwrap();
        assert_eq!(val, 1234.0);
    }

    #[test]
    fn test_int16_negative() {
        let regs = encode_value(-2.0, DataType::Int16, Endian::Big).unwrap();
        assert_eq!(regs, vec![0xFFFE]);
        let val = decode_value(&regs, DataType::Int16, Endian::Big).unwrap();
        assert_eq!(val, -2.0);
    }

    #[test]
    fn test_float32_big_endian() {
        // 25.0 in IEEE 754 = 0x41C80000
        let regs = encode_value(25.0, DataType::Float32, Endian::Big).unwrap();
        assert_eq!(regs, vec![0x41C8, 0x0000]);
        let val = decode_value(&regs, DataType::Float32, Endian::Big).unwrap();
        assert!((val - 25.0).abs() < 0.001);
    }

    #[test]
    fn test_float32_little_endian() {
        // 25.0 = 0x41C80000, Little Endian swaps register order: CD AB
        let regs = encode_value(25.0, DataType::Float32, Endian::Little).unwrap();
        assert_eq!(regs, vec![0x0000, 0x41C8]);
        let val = decode_value(&regs, DataType::Float32, Endian::Little).unwrap();
        assert!((val - 25.0).abs() < 0.001);
    }

    #[test]
    fn test_float32_mid_big_endian() {
        // 25.0 = 0x41C80000 → bytes A=0x41 B=0xC8 C=0x00 D=0x00
        // MidBig = BA DC → reg0=0xC841 reg1=0x0000
        let regs = encode_value(25.0, DataType::Float32, Endian::MidBig).unwrap();
        assert_eq!(regs, vec![0xC841, 0x0000]);
        let val = decode_value(&regs, DataType::Float32, Endian::MidBig).unwrap();
        assert!((val - 25.0).abs() < 0.001);
    }

    #[test]
    fn test_uint32_round_trip() {
        let regs = encode_value(70000.0, DataType::UInt32, Endian::Big).unwrap();
        let val = decode_value(&regs, DataType::UInt32, Endian::Big).unwrap();
        assert_eq!(val, 70000.0);
    }

    #[test]
    fn test_int32_negative() {
        let regs = encode_value(-100000.0, DataType::Int32, Endian::Big).unwrap();
        let val = decode_value(&regs, DataType::Int32, Endian::Big).unwrap();
        assert_eq!(val, -100000.0);
    }

    // --- Validation tests ---

    #[test]
    fn test_uint16_out_of_range() {
        assert!(validate_range(70000.0, DataType::UInt16).is_err());
        assert!(validate_range(-1.0, DataType::UInt16).is_err());
        assert!(validate_range(100.5, DataType::UInt16).is_err());
    }

    #[test]
    fn test_int16_out_of_range() {
        assert!(validate_range(40000.0, DataType::Int16).is_err());
        assert!(validate_range(-40000.0, DataType::Int16).is_err());
    }

    #[test]
    fn test_bool_validation() {
        assert!(validate_range(0.0, DataType::Bool).is_ok());
        assert!(validate_range(1.0, DataType::Bool).is_ok());
        assert!(validate_range(2.0, DataType::Bool).is_err());
    }

    // --- RegisterMap tests ---

    #[test]
    fn test_holding_register_read_write() {
        let mut map = RegisterMap::new();
        map.write_holding_register(0, 1234);
        map.write_holding_register(1, 5678);
        let vals = map.read_holding_registers(0, 3);
        assert_eq!(vals, vec![1234, 5678, 0]); // addr 2 defaults to 0
    }

    #[test]
    fn test_coil_read_write() {
        let mut map = RegisterMap::new();
        map.write_coil(0, true);
        map.write_coil(1, false);
        map.write_coil(2, true);
        assert_eq!(map.read_coils(0, 3), vec![true, false, true]);
    }

    #[test]
    fn test_write_multiple_holding_registers() {
        let mut map = RegisterMap::new();
        map.write_holding_registers(10, &[100, 200, 300]);
        assert_eq!(map.read_holding_registers(10, 3), vec![100, 200, 300]);
    }

    #[test]
    fn test_write_multiple_coils() {
        let mut map = RegisterMap::new();
        map.write_coils(0, &[true, false, true, true]);
        assert_eq!(map.read_coils(0, 4), vec![true, false, true, true]);
    }

    #[test]
    fn remove_and_replace_definition_update_raw_address_lifecycle() {
        let original = RegisterDef {
            address: 10,
            register_type: RegisterType::HoldingRegister,
            data_type: DataType::UInt32,
            endian: Endian::Big,
            name: String::new(),
            comment: String::new(),
            mutation: None,
            data_source: None,
        };
        let replacement = RegisterDef {
            address: 20,
            data_type: DataType::UInt16,
            ..original.clone()
        };
        let mut map = RegisterMap::new();
        map.ensure_from_def(&original);
        map.write_holding_registers(10, &[0x1234, 0x5678]);

        map.replace_def(&original, &replacement);
        assert!(!map.has_holding_register(10));
        assert!(!map.has_holding_register(11));
        assert_eq!(map.holding_registers.get(&20), Some(&0x1234));

        map.remove_from_def(&replacement);
        assert!(!map.has_holding_register(20));
    }
}
