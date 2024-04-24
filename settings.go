package main

import (
	"encoding/json"
	"fmt"
	"github.com/solusio/import-vmware/common"
	"os"
)

const (
	apiURLExample = "https://solusvm2.example.tld/api/v1/"
)

type ImportSettings struct {
	SourceIP string   `json:"source_ip"`
	APIURL   string   `json:"api_url"`
	APIToken string   `json:"api_token"`
	Defaults Defaults `json:"defaults"`
}

type Defaults struct {
	GuestOSToOSImageVersionID map[string]int `json:"guest_os_to_os_image_version_id"`
	UserID                    int            `json:"user_id"`
	ProjectID                 int            `json:"project_id"`
	ComputeResourceID         int            `json:"compute_resource_id"`
	LocationID                int            `json:"location_id"`
	SSHKeys                   []int          `json:"ssh_keys"`
	AdditionalDiskOfferID     int            `json:"additional_disk_offer_id"`
}

func saveSettings(settingsFilePath string, settings ImportSettings) error {
	f, err := os.OpenFile(settingsFilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("open file %q: %w", settingsFilePath, err)
	}

	defer common.CloseWrapper(f)
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(settings); err != nil {
		return fmt.Errorf("failed to write settings file %s: %v", settingsFilePath, err)
	}

	return nil
}

func loadSettings(settingsFilePath string) (ImportSettings, error) {
	var settings ImportSettings
	f, err := os.Open(settingsFilePath)
	if err != nil {
		return settings, fmt.Errorf("failed to open %q: %v", settingsFilePath, err)
	}
	defer common.CloseWrapper(f)
	if err := json.NewDecoder(f).Decode(&settings); err != nil {
		return settings, fmt.Errorf("failed to decode settings file: %v", err)
	}

	return settings, nil
}
