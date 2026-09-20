// Expansion of the legacy block form that early v1 project files used for
// device registers. Ported from legacy_register_defs in
// crates/modbussim-app/src/commands.rs.
package project

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/Karl-Dai/ModbusSim/go/internal/register"
)

func defaultTo[T comparable](value, fallback T) T {
	var zero T
	if value == zero {
		return fallback
	}
	return value
}

func hasBlocks(regs v1Registers) bool {
	return len(regs.Coils) > 0 || len(regs.DiscreteInputs) > 0 || len(regs.Holding) > 0 || len(regs.Input) > 0
}

// expandBlocks turns each {address, count, data_type, endian, values, names}
// block into one point per value slot, and collects the raw values the block
// carried. A 32-bit block yields one point every two addresses.
func expandBlocks(regs v1Registers) ([]*Point, Values, error) {
	groups := []struct {
		registerType register.RegisterType
		blocks       []v1Block
	}{
		{register.Coil, regs.Coils},
		{register.DiscreteInput, regs.DiscreteInputs},
		{register.HoldingRegType, regs.Holding},
		{register.InputRegister, regs.Input},
	}

	var points []*Point
	values := Values{}

	for _, group := range groups {
		bitArea := group.registerType == register.Coil || group.registerType == register.DiscreteInput
		for i := range group.blocks {
			block := &group.blocks[i]

			dataType := register.TypeUInt16
			if bitArea {
				dataType = register.TypeBool
			}
			if block.DataType != nil {
				var err error
				dataType, err = importDataType(*block.DataType)
				if err != nil {
					return nil, Values{}, err
				}
			}
			endian := register.DefaultEndian
			if block.Endian != nil {
				var err error
				endian, err = importEndian(*block.Endian)
				if err != nil {
					return nil, Values{}, err
				}
			}

			stride := uint16(1)
			if !bitArea {
				stride = dataType.RegisterCount()
			}
			if block.Count%stride != 0 {
				return nil, Values{}, fmt.Errorf(
					"legacy register block at %d has count %d which is not divisible by data width %d",
					block.Address, block.Count, stride)
			}

			for offset := uint16(0); offset < block.Count; offset += stride {
				address := block.Address + offset
				if address < block.Address {
					return nil, Values{}, fmt.Errorf("project register address overflow")
				}
				points = append(points, &Point{
					Address:      address,
					RegisterType: group.registerType,
					DataType:     dataType,
					Endian:       endian,
					Name:         blockName(block, address, offset),
				})
			}

			limited := block.Values
			if len(limited) > int(block.Count) {
				limited = limited[:block.Count]
			}
			for offset, raw := range limited {
				address := block.Address + uint16(offset)
				if address < block.Address {
					return nil, Values{}, fmt.Errorf("project register value address overflow")
				}
				if bitArea {
					bit, err := bitValue(raw, address)
					if err != nil {
						return nil, Values{}, err
					}
					if group.registerType == register.Coil {
						values.setCoil(address, bit)
					} else {
						values.setDiscreteInput(address, bit)
					}
					continue
				}
				word, err := wordValue(raw, address)
				if err != nil {
					return nil, Values{}, err
				}
				if group.registerType == register.HoldingRegType {
					values.setHoldingRegister(address, word)
				} else {
					values.setInputRegister(address, word)
				}
			}
		}
	}
	return points, values, nil
}

// blockName resolves a legacy point name: Rust looked the address up as a
// decimal string, then fell back to the offset within the block.
func blockName(block *v1Block, address, offset uint16) string {
	if name, ok := block.Names[strconv.Itoa(int(address))]; ok {
		return name
	}
	if name, ok := block.Names[strconv.Itoa(int(offset))]; ok {
		return name
	}
	return ""
}

func bitValue(raw json.RawMessage, address uint16) (bool, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b, nil
	}
	var n uint64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n != 0, nil
	}
	return false, fmt.Errorf("invalid bit value at legacy address %d", address)
}

func wordValue(raw json.RawMessage, address uint16) (uint16, error) {
	var n uint64
	if err := json.Unmarshal(raw, &n); err == nil && n <= 65535 {
		return uint16(n), nil
	}
	return 0, fmt.Errorf("invalid word value at legacy address %d", address)
}

func (v *Values) setCoil(addr uint16, value bool) {
	if v.Coils == nil {
		v.Coils = map[uint16]bool{}
	}
	v.Coils[addr] = value
}

func (v *Values) setDiscreteInput(addr uint16, value bool) {
	if v.DiscreteInputs == nil {
		v.DiscreteInputs = map[uint16]bool{}
	}
	v.DiscreteInputs[addr] = value
}

func (v *Values) setHoldingRegister(addr, value uint16) {
	if v.HoldingRegisters == nil {
		v.HoldingRegisters = map[uint16]uint16{}
	}
	v.HoldingRegisters[addr] = value
}

func (v *Values) setInputRegister(addr, value uint16) {
	if v.InputRegisters == nil {
		v.InputRegisters = map[uint16]uint16{}
	}
	v.InputRegisters[addr] = value
}
