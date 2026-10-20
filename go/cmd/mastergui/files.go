package main

import (
	"fmt"
	"github.com/Karl-Dai/ModbusSim/go/internal/explain"
	"github.com/Karl-Dai/ModbusSim/go/internal/nativedialog"
	"github.com/Karl-Dai/ModbusSim/go/internal/tools"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type PLCAddress struct {
	ProtocolAddress uint16
	RegisterType    string
}
type FileFilter struct {
	Name       string
	Extensions []string
}

func (a *App) InspectModbusFrame(data, transport, direction, lang string) (string, error) {
	return explain.Explain(data, transport, direction, explain.LangFromCode(lang))
}
func (a *App) ConvertPLCToModbus(address string) (PLCAddress, error) {
	value, err := strconv.ParseUint(strings.TrimSpace(address), 10, 32)
	if err != nil {
		return PLCAddress{}, fmt.Errorf("invalid PLC address: %w", err)
	}
	result, err := tools.PLCToModbus(uint32(value))
	areas := map[tools.AddressType]string{tools.Coil: "coil", tools.DiscreteInput: "discrete_input", tools.InputRegister: "input_register", tools.HoldingReg: "holding_register"}
	return PLCAddress{result.Address, areas[result.Type]}, err
}
func (a *App) CalculateCRC16(data string) (string, error) {
	bytes, err := tools.ParseHexString(data)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%04X", tools.CRC16(bytes)), nil
}
func (a *App) CalculateLRC(data string) (string, error) {
	bytes, err := tools.ParseHexString(data)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%02X", tools.LRC(bytes)), nil
}
func dialogFilters(filters []FileFilter) []runtime.FileFilter {
	out := make([]runtime.FileFilter, 0, len(filters))
	for _, f := range filters {
		patterns := make([]string, 0, len(f.Extensions))
		for _, ext := range f.Extensions {
			patterns = append(patterns, "*."+ext)
		}
		out = append(out, runtime.FileFilter{DisplayName: f.Name, Pattern: strings.Join(patterns, ";")})
	}
	return out
}
func nativeFilters(filters []FileFilter) []nativedialog.Filter {
	out := make([]nativedialog.Filter, 0, len(filters))
	for _, f := range filters {
		out = append(out, nativedialog.Filter{Name: f.Name, Extensions: f.Extensions})
	}
	return out
}
func (a *App) OpenFileDialog(filters []FileFilter) (string, error) {
	return nativedialog.OpenFile(a.ctx, nativeFilters(filters))
}
func (a *App) SaveFileDialog(defaultPath string, filters []FileFilter) (string, error) {
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{DefaultFilename: filepath.Base(defaultPath), Filters: dialogFilters(filters)})
}
func (a *App) SaveTextExport(path, content string) error {
	return os.WriteFile(path, []byte(content), 0600)
}
