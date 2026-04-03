//go:build !windows

package discovery

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ErrNoSerialDevice is returned when no matching serial port is found.
var ErrNoSerialDevice = errors.New("no serial device found")

// knownDevices maps hint name → list of "VVVV:PPPP" VID:PID strings (uppercase hex).
var knownDevices = map[string][]string{
	"esp32": {"10C4:EA60", "1A86:7523"},  // CP2102, CH340
	"ftdi":  {"0403:6001", "0403:6015"},  // FT232R, FT231X
	"gps":   {"067B:2303", "1546:01A7"},  // PL2303, u-blox
}

// FindSerialPort finds a serial port matching the hint on the current OS.
// hint is one of: "esp32", "ftdi", "gps", "any".
// Returns the device path (e.g. "/dev/ttyUSB0").
func FindSerialPort(hint string) (string, error) {
	if runtime.GOOS == "darwin" {
		return findSerialMacOS(hint)
	}
	return FindSerialPortInRoot("/", hint)
}

// FindSerialPortInRoot is the testable version that accepts a sysfs root dir.
// On a real system, call FindSerialPort which passes root="/".
func FindSerialPortInRoot(sysRoot, hint string) (string, error) {
	sysClassTTY := filepath.Join(sysRoot, "sys", "class", "tty")
	devRoot := filepath.Join(sysRoot, "dev")

	// Find all tty entries that have a device subdirectory (i.e. are real hardware).
	entries, _ := filepath.Glob(filepath.Join(sysClassTTY, "*", "device"))
	var candidates []string

	for _, deviceDir := range entries {
		ttyName := filepath.Base(filepath.Dir(deviceDir))
		// Only USB serial devices.
		if !strings.HasPrefix(ttyName, "ttyUSB") && !strings.HasPrefix(ttyName, "ttyACM") {
			continue
		}
		devPath := filepath.Join(devRoot, ttyName)
		if _, err := os.Stat(devPath); err != nil {
			continue
		}

		if hint == "any" {
			candidates = append(candidates, devPath)
			continue
		}

		vendorBytes, err1 := os.ReadFile(filepath.Join(deviceDir, "idVendor"))
		productBytes, err2 := os.ReadFile(filepath.Join(deviceDir, "idProduct"))
		if err1 != nil || err2 != nil {
			continue
		}
		vid := strings.TrimSpace(strings.ToUpper(string(vendorBytes)))
		pid := strings.TrimSpace(strings.ToUpper(string(productBytes)))
		vidpid := vid + ":" + pid

		if serialHintMatches(hint, vidpid) {
			candidates = append(candidates, devPath)
		}
	}

	if len(candidates) == 0 {
		return "", ErrNoSerialDevice
	}
	sort.Strings(candidates)
	return candidates[0], nil
}

// findSerialMacOS finds serial ports on macOS using device name patterns.
// Note: on macOS there is no sysfs to read VID/PID from, so hint-based
// filtering is not available. Any USB serial device matching the glob
// patterns is returned regardless of hint.
func findSerialMacOS(_ string) (string, error) {
	patterns := []string{
		"/dev/tty.SLAB_USBtoUART*",
		"/dev/tty.usbserial-*",
		"/dev/tty.usbmodem*",
	}
	var candidates []string
	for _, p := range patterns {
		matches, _ := filepath.Glob(p)
		candidates = append(candidates, matches...)
	}
	if len(candidates) == 0 {
		return "", ErrNoSerialDevice
	}
	sort.Strings(candidates)
	return candidates[0], nil
}

// serialHintMatches returns true if vidpid matches any known VID:PID for hint.
func serialHintMatches(hint, vidpid string) bool {
	pids, ok := knownDevices[hint]
	if !ok {
		return false
	}
	for _, p := range pids {
		if p == vidpid {
			return true
		}
	}
	return false
}
