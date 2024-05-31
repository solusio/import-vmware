package main

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	libvirtxml "github.com/libvirt/libvirt-go-xml"
	"github.com/solusio/import-vmware/command"
	"github.com/solusio/import-vmware/common"
	"github.com/solusio/solus-go-sdk"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DeviceTypeDisk = "disk"
)

func importDisks(sourceIP string, plan ImportPlan) error {
	// virt-v2v \
	// -i vmx -it ssh \
	// "ssh://root@192.168.192.168/vmfs/volumes/datastore1/wind2k35/wind2k35.vmx" \
	// -o local -of qcow2 -os /var/lib/libvirt/images/123/

	for _, vs := range plan.VirtualServers {
		destinationPath := filepath.Dir(vs.PrimaryDiskDestinationPath)
		importedXMLPath := filepath.Join(destinationPath, vs.OriginName+".xml")
		if !common.IsExists(importedXMLPath) {
			args := []string{
				"-i", "vmx",
				"-it", "ssh",
				fmt.Sprintf("ssh://root@%s%s", sourceIP, vs.VMXFilePath),
				"-o", "local",
				"-of", "qcow2",
				"-os", destinationPath,
			}

			if err := command.DefaultCommander.Build("virt-v2v", args...).Exec(); err != nil {
				return err
			}
		}

		_ = command.DefaultCommander.Build("virsh", "destroy", vs.VirtualServerUUID).Exec()

		disks, err := getDisks(importedXMLPath)
		if err != nil {
			return fmt.Errorf("failed to get disks from %q: %s", importedXMLPath, err)
		}

		if len(disks) == 0 {
			return fmt.Errorf("zero disks found in %q", importedXMLPath)
		}
		if err := os.Rename(disks[0].path, vs.PrimaryDiskDestinationPath); err != nil {
			return fmt.Errorf("failed to move %q to %q: %s", disks[0].path, vs.PrimaryDiskDestinationPath, err)
		}

		if strings.Contains(vs.GuestOS, "windows") {
			baseURL, err := url.Parse(plan.Settings.APIURL)
			if err != nil {
				return fmt.Errorf("parse api url %q: %w", plan.Settings.APIURL, err)
			}

			client, err := solus.NewClient(baseURL, solus.APITokenAuthenticator{Token: plan.Settings.APIToken})
			if err != nil {
				return fmt.Errorf("create client: %w", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
			data := solus.VirtualServerUpdateSettingsRequest{
				DiskDriver: "sata",
			}
			if _, err := client.VirtualServers.UpdateSettings(ctx, vs.VirtualServerID, data); err != nil {
				cancel()
				return fmt.Errorf("update virtual server %d disk driver to sata: %w", vs.VirtualServerID, err)
			}
			cancel()
		}

		if len(vs.AdditionalDisks) > 0 {
			if len(disks) == 1 {
				return fmt.Errorf("virtual server %q additional disks not found in %q, expected additional disks count is %d",
					vs.OriginName,
					importedXMLPath,
					len(vs.AdditionalDisks))
			}

			additionalDisks := disks[1:]

			if len(additionalDisks) != len(vs.AdditionalDisks) {
				return fmt.Errorf("virtual server %q number of additional disks in %q is %d, but %d expected",
					vs.OriginName,
					importedXMLPath,
					len(additionalDisks),
					len(vs.AdditionalDisks))
			}

			for i, disk := range additionalDisks {
				if err := os.Rename(disk.path, vs.AdditionalDisks[i].DestinationPath); err != nil {
					return fmt.Errorf("failed to move virtual server %q disk %q to %q: %s",
						vs.OriginName,
						disk.path,
						vs.AdditionalDisks[i].DestinationPath,
						err)
				}
			}
		}
	}

	return nil
}

type domainDisk struct {
	// Guest disk device name.
	// For example for `<target dev='vda' bus='scsi'/>` it will contains `vda`.
	device string

	// Driver type of the disk.
	// Maybe one of supported but in our case it should be QCOW2 or RAW.
	imageFormat string

	// Path to disk file or block device.
	// For example for `<source file='/var/lib/libvirt/images/1/image'/>` it will
	// contains `/var/lib/libvirt/images/1/image`.
	path string
}

// getDisks return disks of the domain.
func getDisks(path string) ([]domainDisk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	var domainXml libvirtxml.Domain
	err = xml.NewDecoder(f).Decode(&domainXml)
	if err != nil {
		return nil, err
	}

	if domainXml.Devices == nil || len(domainXml.Devices.Disks) == 0 {
		return nil, errors.New("no disks found")
	}

	return buildDisks(domainXml.Devices.Disks)
}

func buildDisks(disks []libvirtxml.DomainDisk) ([]domainDisk, error) {
	expectedLen := len(disks) - 1
	result := make([]domainDisk, 0, expectedLen)

	for _, disk := range disks {
		if !isDiskDevice(disk) {
			continue
		}

		diskPath, err := GetDiskSourcePath(disk.Source)
		if err != nil {
			return nil, err
		}

		d := domainDisk{
			device:      disk.Target.Dev,
			path:        diskPath,
			imageFormat: disk.Driver.Type,
		}

		result = append(result, d)
	}
	return result, nil
}

func isDiskDevice(d libvirtxml.DomainDisk) bool {
	return d.Device == DeviceTypeDisk && d.Source != nil && (d.Source.File != nil || d.Source.Block != nil)
}

func GetDiskSourcePath(s *libvirtxml.DomainDiskSource) (string, error) {
	if s == nil {
		return "", errors.New("disk source is nil")
	}

	if s.File != nil && s.File.File != "" {
		return s.File.File, nil
	}

	if s.Block != nil && s.Block.Dev != "" {
		return s.Block.Dev, nil
	}

	return "", errors.New("disk source is nil or has empty path")
}
