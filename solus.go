package main

import (
	"context"
	"fmt"
	"github.com/solusio/solus-go-sdk"
	"net/url"
	"time"
)

func createVirtualServers(settings ImportSettings, importPlanFilePath string, recreate bool) error {
	plan, err := loadImportPlan(importPlanFilePath)
	if err != nil {
		return err
	}

	plan.Settings = settings

	if err := plan.Validate(); err != nil {
		return fmt.Errorf("failed to validate import plan: %v", err)
	}

	baseURL, err := url.Parse(plan.Settings.APIURL)
	if err != nil {
		return fmt.Errorf("parse api url %q: %w", plan.Settings.APIURL, err)
	}

	client, err := solus.NewClient(baseURL, solus.APITokenAuthenticator{Token: plan.Settings.APIToken})
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	for i, vsPlan := range plan.VirtualServers {
		if vsPlan.VirtualServerID != 0 && !recreate {
			continue
		}

		crID := plan.Settings.Defaults.ComputeResourceID
		if vsPlan.ComputeResourceID != 0 {
			crID = vsPlan.ComputeResourceID
		}

		for i, d := range vsPlan.AdditionalDisks {
			if d.DiskOfferID == 0 {
				vsPlan.AdditionalDisks[i].DiskOfferID = plan.Settings.Defaults.AdditionalDiskOfferID
			}
		}

		data := solus.VirtualServerCreateRequest{
			Name:              vsPlan.Hostname,
			SSHKeys:           append(plan.Settings.Defaults.SSHKeys, vsPlan.SSHKeys...),
			ProjectID:         plan.Settings.Defaults.ProjectID,
			LocationID:        plan.Settings.Defaults.LocationID,
			ComputeResourceID: crID,
			OSImageVersionID:  plan.Settings.Defaults.GuestOSToOSImageVersionID[vsPlan.GuestOS],
			CustomPlan:        &vsPlan.CustomPlan,
			AdditionalIPCount: vsPlan.AdditionalIPv4,
			PrimaryIP:         vsPlan.PrimaryIP,
			AdditionalDisks:   diskToAdditionalDiskCreateRequest(vsPlan.AdditionalDisks),
			MacAddress:        vsPlan.MacAddress,
			Firmware:          vsPlan.Firmware,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		vs, err := client.VirtualServers.Create(ctx, data)
		if err != nil {
			cancel()
			return fmt.Errorf("create virtual server %q: %w", vsPlan.Hostname, err)
		}
		cancel()

		fmt.Printf("Virtual server %q created as ID %d\n", vsPlan.Hostname, vs.ID)

		ctx, cancel = context.WithTimeout(context.Background(), 35*time.Second)
		disks, err := client.VirtualServers.Disks(ctx, vs.ID)
		if err != nil {
			cancel()
			return fmt.Errorf("create virtual server %q: %w", vsPlan.Hostname, err)
		}
		cancel()

		plan.VirtualServers[i].VirtualServerUUID = vs.UUID
		plan.VirtualServers[i].VirtualServerID = vs.ID

		for _, dest := range disks {
			if dest.IsPrimary {
				plan.VirtualServers[i].PrimaryDiskDestinationPath = dest.FullPath
				continue
			}

			for y, source := range plan.VirtualServers[i].AdditionalDisks {
				if dest.Name == source.Name {
					plan.VirtualServers[i].AdditionalDisks[y].DestinationPath = dest.FullPath
				}
			}
		}

		if err := saveImportPlan(importPlanFilePath, plan); err != nil {
			return fmt.Errorf("save import plan: %w", err)
		}
	}

	return nil
}

func diskToAdditionalDiskCreateRequest(disks []Disk) []solus.AdditionalDiskCreateRequest {
	var createRequest []solus.AdditionalDiskCreateRequest
	for _, disk := range disks {
		createRequest = append(createRequest, solus.AdditionalDiskCreateRequest{
			OfferID: disk.DiskOfferID,
			Size:    disk.Size,
			Name:    disk.Name,
		})
	}
	return createRequest
}

func fillSolusPlanDefaults(plan solus.Plan) solus.Plan {
	plan.BackupSettings.IncrementalBackupsLimit = 3
	plan.Params.VCPUUnits = 1000
	plan.Params.VCPULimit = 100
	plan.Params.IOPriority = 4
	plan.ResetLimitPolicy = "never"
	plan.NetworkTotalTrafficType = "separate"
	plan.Netfilter.Value = "full"
	plan.Limits.DiskIOPS.Unit = "iops"
	plan.Limits.DiskBandwidth.Unit = "Bps"
	plan.Limits.BackupsNumber.Unit = "units"
	plan.Limits.NetworkIncomingBandwidth.Unit = "Mbps"
	plan.Limits.NetworkOutgoingBandwidth.Unit = "Mbps"
	plan.Limits.NetworkIncomingTraffic.Unit = "GiB"
	plan.Limits.NetworkOutgoingTraffic.Unit = "GiB"
	plan.Limits.NetworkTotalTraffic.Unit = "GiB"
	plan.Limits.NetworkReduceBandwidth.Unit = "Kbps"
	return plan
}
