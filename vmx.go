package main

import (
	"fmt"
	"github.com/solusio/import-vmware/common"
	vmx "github.com/solusio/import-vmware/govmx"
	"github.com/solusio/solus-go-sdk"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type VMXFile struct {
	ParentPath string
	Name       string `vmx:"displayName"`
	//numvcpus = "2"
	Numvcpus int `vmx:"numvcpus"`
	// memSize = "2048"
	MemSize int `vmx:"memsize"`
	// guestOS = "debian12-64"
	// guestOS = "windows2022srvNext-64"
	GuestOS string `vmx:"guestOS"`
	// firmware = "efi"
	Firmware    string           `vmx:"firmware"`
	IDEDevices  []vmx.IDEDevice  `vmx:"ide,omitempty"`
	SCSIDevices []vmx.SCSIDevice `vmx:"scsi,omitempty"`
	SATADevices []vmx.SATADevice `vmx:"sata,omitempty"`
	NVMEDevices []vmx.NVMEDevice `vmx:"nvme,omitempty"`
	Ethernet    []vmx.Ethernet   `vmx:"ethernet,omitempty"`
}

func ParseVMWareVirtualServerPath(path string) (VirtualServer, error) {
	dir, err := os.ReadDir(path)
	if err != nil {
		return VirtualServer{}, err
	}

	var vmxFilePath string
	var disksPaths []string
	for _, f := range dir {
		if f.IsDir() {
			continue
		}

		if filepath.Ext(f.Name()) == ".vmx" {
			vmxFilePath = filepath.Join(path, f.Name())
		}

		if filepath.Ext(f.Name()) == ".vmdk" {
			disksPaths = append(disksPaths, f.Name())
		}

	}

	if vmxFilePath == "" {
		return VirtualServer{}, fmt.Errorf("no vmx file found in %s", path)
	}

	if len(disksPaths) == 0 {
		return VirtualServer{}, fmt.Errorf("no vmdk files found in %s", path)
	}

	vmxFile, err := ParseVMXFile(vmxFilePath)
	if err != nil {
		return VirtualServer{}, err
	}

	if vmxFile.Firmware == "" {
		vmxFile.Firmware = "bios"
	}

	primaryDisk, additionalDisks, err := GetDisksFromVMX(vmxFile)
	if err != nil {
		return VirtualServer{}, fmt.Errorf("failed to get disks from vmx file %q: %w", vmxFilePath, err)
	}

	plan := solus.Plan{
		ID:                 0,
		Name:               "",
		VirtualizationType: solus.VirtualizationTypeKVM,
		StorageType:        "fb",
		ImageFormat:        "qcow2",
		Params: solus.PlanParams{
			Disk: int(primaryDisk.Size),
			RAM:  vmxFile.MemSize * 1024,
			VCPU: vmxFile.Numvcpus,
		},
		IsDefault:                false,
		IsSnapshotAvailable:      false,
		IsSnapshotsEnabled:       false,
		IsBackupAvailable:        false,
		IsAdditionalIPsAvailable: false,
	}

	var macAddress *string

	for _, e := range vmxFile.Ethernet {
		if e.AddressType == vmx.MAC_TYPE_GENERATED && e.GeneratedAddress != "" {
			macAddress = &e.GeneratedAddress
		}
		if e.AddressType == vmx.MAC_TYPE_STATIC && e.Address != "" {
			macAddress = &e.Address
		}
	}

	return VirtualServer{
		VMXFilePath:           vmxFilePath,
		OriginDir:             filepath.Dir(vmxFilePath),
		OriginName:            vmxFile.Name,
		Hostname:              vmxNameToHostname(vmxFile.Name),
		GuestOS:               vmxFile.GuestOS,
		CustomPlan:            plan,
		PrimaryDiskSourcePath: primaryDisk.SourcePath,
		AdditionalDisks:       additionalDisks,
		MacAddress:            macAddress,
		Firmware:              &vmxFile.Firmware,
	}, nil
}

func ParseVMXFile(path string) (VMXFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return VMXFile{}, err
	}
	v := VMXFile{}

	if err := vmx.Unmarshal(b, &v); err != nil {
		return VMXFile{}, fmt.Errorf("failed to unmarshal vmx file %q: %v", path, err)
	}

	v.ParentPath = filepath.Dir(path)
	v.MemSize = v.MemSize * 1024
	if v.Numvcpus == 0 {
		v.Numvcpus = 1
	}

	return v, nil
}

// GetDisksFromVMX returns primary and additional disks from VMX file.
func GetDisksFromVMX(v VMXFile) (Disk, []Disk, error) {
	primary := Disk{}
	var additional []Disk

	var devices []vmx.Device

	for _, ide := range v.IDEDevices {
		devices = append(devices, ide.Device)
	}

	for _, sata := range v.SATADevices {
		if sata.Type == "cdrom-image" {
			continue
		}

		devices = append(devices, sata.Device)
	}

	for _, scsi := range v.SCSIDevices {
		if scsi.Type != "scsi-hardDisk" {
			continue
		}

		devices = append(devices, scsi.Device)
	}

	for _, nvme := range v.NVMEDevices {
		devices = append(devices, nvme.Device)
	}

	originVMName := filepath.Base(v.ParentPath)

	for _, dev := range devices {
		if dev.Filename == "" {
			continue
		}

		fullPath := filepath.Join(v.ParentPath, dev.Filename)
		if !common.IsExists(fullPath) {
			continue
		}

		if strings.Contains(dev.Filename, "-0") { // scsi0:1.fileName = "testvm_1-000001.vmdk"
			return Disk{}, nil, fmt.Errorf("disk filename %q looks like has a snapshot", dev.Filename)
		}

		size, err := getDiskSizeGiB(fullPath)
		if err != nil {
			return Disk{}, nil, err
		}

		// Determine -flat file to get actual size.
		flatPath := strings.Replace(fullPath, ".vmdk", "-flat.vmdk", 1)
		if common.IsExists(flatPath) {
			flatSize, err := getDiskSizeGiB(flatPath)
			if err != nil {
				return Disk{}, nil, err
			}
			size = flatSize
		}

		disk := Disk{
			Name:       fullPath,
			SourcePath: fullPath,
			Size:       size,
		}

		log.Println(strings.TrimSuffix(dev.Filename, ".vmdk"), originVMName)

		if strings.TrimSuffix(dev.Filename, ".vmdk") == originVMName { // sata0:1.fileName = "test vm.vmdk"
			primary = disk
			continue
		}

		additional = append(additional, disk)
	}

	if primary.SourcePath == "" {
		return Disk{}, nil, fmt.Errorf("primary disk not found")
	}

	return primary, additional, nil
}

func getDiskSizeGiB(path string) (int, error) {
	size, err := common.GetSize(path)
	if err != nil {
		return 0, fmt.Errorf("failed to get size of file %q: %w", path, err)
	}

	return int(size / 1024 / 1024 / 1024), nil
}

func vmxNameToHostname(name string) string {
	name = strings.TrimLeft(name, " ")
	name = strings.TrimRight(name, " ")
	name = strings.Replace(name, " ", "-", -1)
	name = strings.Replace(name, "_", "-", -1)
	name = strings.ToLower(name)
	return name
}
