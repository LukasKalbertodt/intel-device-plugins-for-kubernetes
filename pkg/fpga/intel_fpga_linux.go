// Copyright 2019 Intel Corporation. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package fpga

import (
	"math"
	"path/filepath"
	"strconv"
	"unsafe"

	"github.com/intel/intel-device-plugins-for-kubernetes/pkg/fpga/bitstream"
	"github.com/pkg/errors"
)

const (
	intelFpgaFmePrefix  = "intel-fpga-fme."
	intelFpgaPortPrefix = "intel-fpga-port."
	intelFpgaFmeGlobPCI = "fpga/intel-fpga-dev.*/intel-fpga-fme.*"
)

// IntelFpgaFME represent Intel FPGA FME device.
type IntelFpgaFME struct {
	FME
	DevPath           string
	SysFsPath         string
	Name              string
	PCIDevice         *PCIDevice
	SocketID          string
	Dev               string
	CompatID          string
	BitstreamID       string
	BitstreamMetadata string
	PortsNum          string
}

// Close closes open device.
func (f *IntelFpgaFME) Close() error {
	return nil
}

// NewIntelFpgaFME Opens device.
func NewIntelFpgaFME(dev string) (FME, error) {
	fme := &IntelFpgaFME{DevPath: dev}
	if err := checkPCIDeviceType(fme); err != nil {
		return nil, err
	}

	if err := fme.updateProperties(); err != nil {
		return nil, err
	}

	return fme, nil
}

// IntelFpgaPort represent IntelFpga FPGA Port device.
type IntelFpgaPort struct {
	Port
	FME       FME
	DevPath   string
	SysFsPath string
	Name      string
	PCIDevice *PCIDevice
	Dev       string
	AFUID     string
	ID        string
}

// Close closes open device.
func (f *IntelFpgaPort) Close() error {
	if f.FME != nil {
		defer f.FME.Close()
	}

	return nil
}

// NewIntelFpgaPort Opens device.
func NewIntelFpgaPort(dev string) (Port, error) {
	port := &IntelFpgaPort{DevPath: dev}
	if err := checkPCIDeviceType(port); err != nil {
		port.Close()
		return nil, err
	}

	if err := port.updateProperties(); err != nil {
		port.Close()
		return nil, err
	}

	return port, nil
}

// common ioctls for FME and Port.
func commonIntelFpgaGetAPIVersion(fd string) (int, error) {
	v, err := ioctlDev(fd, FPGA_GET_API_VERSION, 0)
	return int(v), err
}
func commonIntelFpgaCheckExtension(fd string) (int, error) {
	v, err := ioctlDev(fd, FPGA_CHECK_EXTENSION, 0)
	return int(v), err
}

// GetAPIVersion  Report the version of the driver API.
// * Return: Driver API Version.
func (f *IntelFpgaFME) GetAPIVersion() (int, error) {
	return commonIntelFpgaGetAPIVersion(f.DevPath)
}

// CheckExtension Check whether an extension is supported.
// * Return: 0 if not supported, otherwise the extension is supported.
func (f *IntelFpgaFME) CheckExtension() (int, error) {
	return commonIntelFpgaCheckExtension(f.DevPath)
}

// GetAPIVersion  Report the version of the driver API.
// * Return: Driver API Version.
func (f *IntelFpgaPort) GetAPIVersion() (int, error) {
	return commonIntelFpgaGetAPIVersion(f.DevPath)
}

// CheckExtension Check whether an extension is supported.
// * Return: 0 if not supported, otherwise the extension is supported.
func (f *IntelFpgaPort) CheckExtension() (int, error) {
	return commonIntelFpgaCheckExtension(f.DevPath)
}

// FME interfaces

// PortPR does Partial Reconfiguration based on Port ID and Buffer (Image)
// provided by caller.
//   - Return: 0 on success, -errno on failure.
//   - If IntelFpga_FPGA_FME_PORT_PR returns -EIO, that indicates the HW has detected
//     some errors during PR, under this case, the user can fetch HW error info
//     from the status of FME's fpga manager.
func (f *IntelFpgaFME) PortPR(port uint32, bitstream []byte) error {
	var value IntelFpgaFmePortPR

	value.Argsz = uint32(unsafe.Sizeof(value))
	value.Port_id = port
	value.Buffer_size = uint32(len(bitstream))
	value.Buffer_address = uint64(uintptr(unsafe.Pointer(&bitstream[0])))

	_, err := ioctlDev(f.DevPath, FPGA_FME_PORT_PR, uintptr(unsafe.Pointer(&value)))

	return err
}

// PortRelease releases the port per Port ID provided by caller.
// * Return: 0 on success, -errno on failure.
func (f *IntelFpgaFME) PortRelease(port uint32) error {
	var value IntelFpgaFmePortRelease

	value.Argsz = uint32(unsafe.Sizeof(value))
	value.Id = port

	_, err := ioctlDev(f.DevPath, FPGA_FME_PORT_RELEASE, uintptr(unsafe.Pointer(&value)))

	return err
}

// PortAssign assigns the port back per Port ID provided by caller.
// * Return: 0 on success, -errno on failure.
func (f *IntelFpgaFME) PortAssign(port uint32) error {
	var value IntelFpgaFmePortAssign

	value.Argsz = uint32(unsafe.Sizeof(value))
	value.Id = port

	_, err := ioctlDev(f.DevPath, FPGA_FME_PORT_ASSIGN, uintptr(unsafe.Pointer(&value)))

	return err
}

// GetDevPath returns path to device node.
func (f *IntelFpgaFME) GetDevPath() string {
	return f.DevPath
}

// GetSysFsPath returns sysfs entry for FPGA FME or Port (e.g. can be used for custom errors/perf items).
func (f *IntelFpgaFME) GetSysFsPath() string {
	if f.SysFsPath != "" {
		return f.SysFsPath
	}

	sysfs, err := FindSysFsDevice(f.DevPath)
	if err != nil {
		return ""
	}

	f.SysFsPath = sysfs

	return f.SysFsPath
}

// GetName returns simple FPGA name, derived from sysfs entry, can be used with /dev/ or /sys/bus/platform/.
func (f *IntelFpgaFME) GetName() string {
	if f.Name != "" {
		return f.Name
	}

	f.Name = filepath.Base(f.GetSysFsPath())

	return f.Name
}

// GetPCIDevice returns PCIDevice for this device.
func (f *IntelFpgaFME) GetPCIDevice() (*PCIDevice, error) {
	if f.PCIDevice != nil {
		return f.PCIDevice, nil
	}

	pci, err := NewPCIDevice(f.GetSysFsPath())
	if err != nil {
		return nil, err
	}

	f.PCIDevice = pci

	return f.PCIDevice, nil
}

// GetPortsNum returns amount of FPGA Ports associated to this FME.
func (f *IntelFpgaFME) GetPortsNum() int {
	if f.PortsNum == "" {
		err := f.updateProperties()
		if err != nil {
			return -1
		}
	}

	n, err := strconv.ParseUint(f.PortsNum, 10, 32)
	if err != nil {
		return -1
	}

	return int(n)
}

// GetInterfaceUUID returns Interface UUID for FME.
func (f *IntelFpgaFME) GetInterfaceUUID() (id string) {
	if f.CompatID == "" {
		err := f.updateProperties()
		if err != nil {
			return ""
		}
	}

	return f.CompatID
}

// GetSocketID returns physical socket number, in case NUMA enumeration fails.
func (f *IntelFpgaFME) GetSocketID() (uint32, error) {
	if f.SocketID == "" {
		return math.MaxUint32, errors.Errorf("n/a")
	}

	id, err := strconv.ParseUint(f.SocketID, 10, 32)

	return uint32(id), err
}

// GetBitstreamID returns FME bitstream id.
func (f *IntelFpgaFME) GetBitstreamID() string {
	return f.BitstreamID
}

// GetBitstreamMetadata returns FME bitstream metadata.
func (f *IntelFpgaFME) GetBitstreamMetadata() string {
	return f.BitstreamMetadata
}

// Update properties from sysfs.
func (f *IntelFpgaFME) updateProperties() error {
	pci, err := f.GetPCIDevice()
	if err != nil {
		return err
	}

	fileMap := map[string]*string{
		"bitstream_id":       &f.BitstreamID,
		"bitstream_metadata": &f.BitstreamMetadata,
		"dev":                &f.Dev,
		"ports_num":          &f.PortsNum,
		"socket_id":          &f.SocketID,
		"pr/interface_id":    &f.CompatID,
	}

	return readFilesInDirectory(fileMap, filepath.Join(pci.SysFsPath, intelFpgaFmeGlobPCI))
}

// Port interfaces

// PortReset Reset the FPGA Port and its AFU. No parameters are supported.
// Userspace can do Port reset at any time, e.g. during DMA or PR. But
// it should never cause any system level issue, only functional failure
// (e.g. DMA or PR operation failure) and be recoverable from the failure.
// * Return: 0 on success, -errno of failure.
func (f *IntelFpgaPort) PortReset() error {
	_, err := ioctlDev(f.DevPath, FPGA_PORT_RESET, 0)
	return err
}

// PortGetInfo Retrieve information about the fpga port.
// Driver fills the info in provided struct IntelFpga_fpga_port_info.
// * Return: 0 on success, -errno on failure.
func (f *IntelFpgaPort) PortGetInfo() (ret PortInfo, err error) {
	var value IntelFpgaPortInfo

	value.Argsz = uint32(unsafe.Sizeof(value))

	_, err = ioctlDev(f.DevPath, FPGA_PORT_GET_INFO, uintptr(unsafe.Pointer(&value)))
	if err == nil {
		ret.Flags = value.Flags
		ret.Regions = value.Regions
		ret.Umsgs = value.Umsgs
	}

	return
}

// PortGetRegionInfo Retrieve information about the fpga port.
// * Retrieve information about a device memory region.
// * Caller provides struct IntelFpga_fpga_port_region_info with index value set.
// * Driver returns the region info in other fields.
// * Return: 0 on success, -errno on failure.
func (f *IntelFpgaPort) PortGetRegionInfo(index uint32) (ret PortRegionInfo, err error) {
	var value IntelFpgaPortRegionInfo

	value.Argsz = uint32(unsafe.Sizeof(value))
	value.Index = index

	_, err = ioctlDev(f.DevPath, FPGA_PORT_GET_REGION_INFO, uintptr(unsafe.Pointer(&value)))
	if err == nil {
		ret.Flags = value.Flags
		ret.Index = value.Index
		ret.Offset = value.Offset
		ret.Size = value.Size
	}

	return
}

// GetDevPath returns path to device node.
func (f *IntelFpgaPort) GetDevPath() string {
	return f.DevPath
}

// GetSysFsPath returns sysfs entry for FPGA FME or Port (e.g. can be used for custom errors/perf items).
func (f *IntelFpgaPort) GetSysFsPath() string {
	if f.SysFsPath != "" {
		return f.SysFsPath
	}

	sysfs, err := FindSysFsDevice(f.DevPath)
	if err != nil {
		return ""
	}

	f.SysFsPath = sysfs

	return f.SysFsPath
}

// GetName returns simple FPGA name, derived from sysfs entry, can be used with /dev/ or /sys/bus/platform/.
func (f *IntelFpgaPort) GetName() string {
	if f.Name != "" {
		return f.Name
	}

	f.Name = filepath.Base(f.GetSysFsPath())

	return f.Name
}

// GetPCIDevice returns PCIDevice for this device.
func (f *IntelFpgaPort) GetPCIDevice() (*PCIDevice, error) {
	if f.PCIDevice != nil {
		return f.PCIDevice, nil
	}

	pci, err := NewPCIDevice(f.GetSysFsPath())
	if err != nil {
		return nil, err
	}

	f.PCIDevice = pci

	return f.PCIDevice, nil
}

// GetFME returns FPGA FME device for this port.
func (f *IntelFpgaPort) GetFME() (fme FME, err error) {
	if f.FME != nil {
		return f.FME, nil
	}

	pci, err := f.GetPCIDevice()
	if err != nil {
		return
	}

	if pci.PhysFn != nil {
		pci = pci.PhysFn
	}

	var dev string

	fileMap := map[string]*string{
		"dev": &dev,
	}
	if err = readFilesInDirectory(fileMap, filepath.Join(pci.SysFsPath, intelFpgaFmeGlobPCI)); err != nil {
		return
	}

	realDev, err := filepath.EvalSymlinks(filepath.Join("/dev/char", dev))
	if err != nil {
		return
	}

	fme, err = NewIntelFpgaFME(realDev)
	if err != nil {
		return
	}

	f.FME = fme

	return fme, err
}

// GetPortID returns ID of the FPGA port within physical device.
func (f *IntelFpgaPort) GetPortID() (uint32, error) {
	if f.ID == "" {
		err := f.updateProperties()
		if err != nil {
			return math.MaxUint32, err
		}
	}

	id, err := strconv.ParseUint(f.ID, 10, 32)

	return uint32(id), err
}

// GetAcceleratorTypeUUID returns AFU UUID for port.
func (f *IntelFpgaPort) GetAcceleratorTypeUUID() string {
	err := f.updateProperties()
	if err != nil || f.AFUID == "" {
		return ""
	}

	return f.AFUID
}

// GetInterfaceUUID returns Interface UUID for FME.
func (f *IntelFpgaPort) GetInterfaceUUID() (id string) {
	fme, err := f.GetFME()
	if err != nil {
		return ""
	}
	defer fme.Close()

	return fme.GetInterfaceUUID()
}

// PR programs specified bitstream to port.
func (f *IntelFpgaPort) PR(bs bitstream.File, dryRun bool) error {
	return genericPortPR(f, bs, dryRun)
}

// Update properties from sysfs.
func (f *IntelFpgaPort) updateProperties() error {
	fileMap := map[string]*string{
		"afu_id": &f.AFUID,
		"dev":    &f.Dev,
		"id":     &f.ID,
	}

	return readFilesInDirectory(fileMap, f.GetSysFsPath())
}
