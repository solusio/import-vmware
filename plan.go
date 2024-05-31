package main

import (
	"encoding/json"
	"fmt"
	"github.com/solusio/import-vmware/common"
	"github.com/solusio/solus-go-sdk"
	"os"
)

type ImportPlan struct {
	Settings       ImportSettings  `json:"-"`
	VirtualServers []VirtualServer `json:"virtual_servers,omitempty"`
}

type VirtualServer struct {
	VMXFilePath                string          `json:"vmx_file_path,omitempty"`
	VirtualServerID            int             `json:"virtual_server_id,omitempty"`
	VirtualServerUUID          string          `json:"virtual_server_uuid,omitempty"`
	OriginDir                  string          `json:"origin_dir,omitempty"`
	OriginName                 string          `json:"origin_name,omitempty"`
	Hostname                   string          `json:"hostname,omitempty"`
	ComputeResourceID          int             `json:"compute_resource_id,omitempty"`
	GuestOS                    string          `json:"guest_os,omitempty"`
	CustomPlan                 solus.Plan      `json:"custom_plan"`
	PrimaryDiskSourcePath      string          `json:"primary_disk_source_path,omitempty"`
	PrimaryDiskDestinationPath string          `json:"primary_disk_destination_path,omitempty"`
	AdditionalDisks            []Disk          `json:"additional_disks,omitempty"`
	PrimaryIP                  *string         `json:"primary_ip,omitempty"`
	AdditionalIPv4             *int            `json:"additional_ipv4,omitempty"`
	Password                   string          `json:"password,omitempty"`
	SSHKeys                    []int           `json:"ssh_keys,omitempty"`
	MacAddress                 *string         `json:"mac_address,omitempty"`
	Firmware                   *solus.Firmware `json:"firmware,omitempty"`
}

type Disk struct {
	Name            string `json:"name,omitempty"`
	DiskOfferID     int    `json:"disk_offer_id,omitempty"`
	Size            int    `json:"size,omitempty"`
	SourcePath      string `json:"source_path,omitempty"`
	DestinationPath string `json:"destination_path,omitempty"`
}

func (i *ImportPlan) Validate() error {
	if i.Settings.APIURL == "" {
		return fmt.Errorf("API URL is not set")
	}
	if i.Settings.APIURL == apiURLExample {
		return fmt.Errorf("API URL is set to example value")
	}
	if i.Settings.APIToken == "" {
		return fmt.Errorf("API token is not set")
	}

	for _, vs := range i.VirtualServers {
		imageID, ok := i.Settings.Defaults.GuestOSToOSImageVersionID[vs.GuestOS]
		if !ok {
			return fmt.Errorf("virtual server's %s guest OS %s does not exist in settings guest_os_to_os_image_version_id", vs.Hostname, vs.GuestOS)
		}

		if imageID == 0 {
			return fmt.Errorf("virtual server's %s guest OS %s does not mapped to OS image ID in settings guest_os_to_os_image_version_id", vs.Hostname, vs.GuestOS)
		}

		if vs.ComputeResourceID == 0 && i.Settings.Defaults.ComputeResourceID == 0 {
			return fmt.Errorf("virtual server's %s compute resource ID is not set and default compute resource ID is not set", vs.Hostname)
		}

		for _, disk := range vs.AdditionalDisks {
			if disk.DiskOfferID == 0 && i.Settings.Defaults.AdditionalDiskOfferID == 0 {
				return fmt.Errorf("virtual server's %s has additional disk %s but disk offer ID is 0. "+
					"Please create addtional disk offer and set disk offer ID for the server "+
					"or set Default Additional Disk Offer ID in \"Defaults\" struct in settings file", vs.Hostname, disk.SourcePath)
			}

			if disk.Size == 0 {
				return fmt.Errorf("virtual server's %s has additional disk %s but size is 0. "+
					"Please set \"Size\" for the server", vs.Hostname, disk.SourcePath)
			}
		}
	}

	if i.Settings.Defaults.UserID == 0 {
		return fmt.Errorf("\"Default User ID\" is not set in settings file")
	}
	if i.Settings.Defaults.ProjectID == 0 {
		return fmt.Errorf("\"Default Project ID\" is not set in settings file")
	}
	if i.Settings.Defaults.ComputeResourceID == 0 {
		return fmt.Errorf("\"Default ComputeResource ID\" is not set in settings file")
	}
	if i.Settings.Defaults.LocationID == 0 {
		return fmt.Errorf("\"Default Location ID\" is not set in settings file")
	}

	return nil
}

func saveImportPlan(importPlanFilePath string, plan ImportPlan) error {
	f, err := os.OpenFile(importPlanFilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("open file %q: %w", importPlanFilePath, err)
	}

	defer common.CloseWrapper(f)
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(plan); err != nil {
		return fmt.Errorf("failed to write import plan file %s: %v", importPlanFilePath, err)
	}

	return nil
}

func loadImportPlan(importPlanFilePath string) (ImportPlan, error) {
	var plan ImportPlan
	f, err := os.Open(importPlanFilePath)
	if err != nil {
		return plan, fmt.Errorf("failed to open %q: %v", importPlanFilePath, err)
	}
	if err := json.NewDecoder(f).Decode(&plan); err != nil {
		return plan, fmt.Errorf("failed to decode import plan: %v", err)
	}

	if err := f.Close(); err != nil {
		return plan, fmt.Errorf("failed to close %q: %v", importPlanFilePath, err)
	}

	return plan, nil
}
