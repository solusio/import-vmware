package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/solusio/import-vmware/common"
	"github.com/solusio/import-vmware/ssh"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const (
	createImportPlanFlagName                 = "create-import-plan"
	storagePathFlagName                      = "storage-path"
	vmDirFlagName                            = "vm-dir"
	sourceIPFlagName                         = "source-ip"
	privateKeyFlagName                       = "private-key"
	createVirtualServersByImportPlanFlagName = "create-virtual-servers-by-import-plan"
	importPlanFilePathFlagName               = "import-plan-file-path"
	settingsFilePathFlagName                 = "settings-file-path"
	recreateVirtualServersFlagName           = "recreate-virtual-servers"
	importDisksFlagName                      = "import-disks"
)

func main() {
	// Step 1
	createImportPlanFlag := flag.Bool(createImportPlanFlagName, false, "Create import plan for virtual servers in storage-path.")
	sourceIPFlag := flag.String(sourceIPFlagName, "", "Source IP or hostname where virtual servers will be imported from.")
	privateKeyFlag := flag.String(privateKeyFlagName, "/root/.ssh/id_rsa", "Private key file path.")
	storagePathFlag := flag.String(storagePathFlagName, "", "Storage path.")
	vmDirFlag := flag.String(vmDirFlagName, "", "Optional. When provided, operation performed for a virtual server stored in that directory only.")

	// Step 1
	createSettingsFileFlag := flag.Bool("create-settings-file", false, "Create settings file example.")
	settingsFilePathFlag := flag.String(settingsFilePathFlagName, "settings.json", "Settings file path.")

	// Step 3
	createVirtualServersFlag := flag.Bool(createVirtualServersByImportPlanFlagName, false, "Create virtual servers in SolusVM 2 by import plan.")
	recreateVirtualServersFlag := flag.Bool(recreateVirtualServersFlagName, false, "Recreate virtual servers in SolusVM 2 by import plan.")
	importPlanFilePathFlag := flag.String(importPlanFilePathFlagName, "import_plan.json", "Import plan file path.")

	// Step 4
	importDisksFlag := flag.Bool(importDisksFlagName, false, "Copy and convert virtual servers disks by import plan from remote source storage path to local destination path.")
	flag.Parse()

	if *createSettingsFileFlag {
		if common.IsExists(*settingsFilePathFlag) {
			log.Fatalf("settings file already exists at %s", *settingsFilePathFlag)
		}

		computeResourceID := 1

		if f, err := os.Open("/etc/solus/agent.json"); err == nil {
			agentConfig := struct {
				ComputeResourceID int `json:"computer_resource_id"`
			}{}
			if err := json.NewDecoder(f).Decode(&agentConfig); err != nil {
				log.Fatalf("failed to decode /etc/solus/agent.json: %s", err)
			}
			computeResourceID = agentConfig.ComputeResourceID
		}

		plan, _ := loadImportPlan(*importPlanFilePathFlag)

		settings := ImportSettings{
			SourceIP: *sourceIPFlag,
			APIURL:   apiURLExample,
			APIToken: "",
			Defaults: Defaults{
				GuestOSToOSImageVersionID: createGuestOSoOSImageVersionID(plan),
				UserID:                    1,
				ProjectID:                 1,
				ComputeResourceID:         computeResourceID,
				LocationID:                1,
				SSHKeys:                   []int{},
				AdditionalDiskOfferID:     0,
			},
		}

		if err := saveSettings(*settingsFilePathFlag, settings); err != nil {
			log.Fatalf("failed to create settings file: %v", err)
		}

		return
	}

	if *createImportPlanFlag {
		if *storagePathFlag == "" {
			log.Fatal("storage-path is empty")
		}

		if *sourceIPFlag == "" {
			importPlan, err := createImportPlan(*storagePathFlag, *vmDirFlag)
			if err != nil {
				log.Fatalf("failed to create import plan: %v", err)
			}

			if err := saveImportPlan(*importPlanFilePathFlag, importPlan); err != nil {
				log.Fatalf("failed to create import plan file: %v", err)
			}

			return
		}

		node, err := ssh.NewNodeConnection(*sourceIPFlag, 22, "root", *privateKeyFlag)
		if err != nil {
			log.Fatalf("failed to create node connection: %v", err)
		}

		if err := node.UploadAgent(); err != nil {
			log.Fatalf("failed to upload agent: %v", err)
		}

		importFilePath := filepath.Join(*storagePathFlag, filepath.Base(*importPlanFilePathFlag))
		args := fmt.Sprintf("-%s -%s %s -%s %q", createImportPlanFlagName, importPlanFilePathFlagName, importFilePath, storagePathFlagName, *storagePathFlag)
		if *vmDirFlag != "" {
			args += fmt.Sprintf(" -%s %q", vmDirFlagName, *vmDirFlag)
		}
		if out, err := node.ExecAgent(args); err != nil {
			log.Fatalf("failed to exec agent %s: %v", string(out), err)
		}

		if err := node.DownloadFile(importFilePath, *importPlanFilePathFlag); err != nil {
			log.Fatalf("failed to download import plan file: %v", err)
		}

		log.Printf("created import plan file, you can use like %s -%s -%s %s",
			os.Args[0], createVirtualServersByImportPlanFlagName, importPlanFilePathFlagName, *importPlanFilePathFlag)

		return
	}

	if *createVirtualServersFlag {
		if *importPlanFilePathFlag == "" {
			log.Fatal("import-plan-file-path is empty")
		}

		settings, err := loadSettings(*settingsFilePathFlag)
		if err != nil {
			log.Fatalf("failed to load settings: %v", err)
		}

		if err := createVirtualServers(settings, *importPlanFilePathFlag, *recreateVirtualServersFlag); err != nil {
			log.Fatalf("failed to create virtual servers: %v", err)
		}

		// ./vmware-importer -import-disks -source-ip 37.27.122.43 -private-key ~/.ssh/id_rsa -import-plan-file-path win2k22.json
		log.Printf("Virtual servers are created, you can import disks like %s -%s -%s %s",
			os.Args[0], importDisksFlagName, importPlanFilePathFlagName, *importPlanFilePathFlag)
		return
	}

	if *importDisksFlag {
		settings, err := loadSettings(*settingsFilePathFlag)
		if err != nil {
			log.Fatalf("failed to load settings: %v", err)
		}

		if *sourceIPFlag == "" && settings.SourceIP == "" {
			log.Fatalf("Source IP not provided with flag -%s or settings file", sourceIPFlagName)
		}

		sourceIP := settings.SourceIP
		if *sourceIPFlag != "" {
			sourceIP = *sourceIPFlag
		}

		plan, err := loadImportPlan(*importPlanFilePathFlag)
		if err != nil {
			log.Fatalf("failed to load import plan: %v", err)
		}

		plan.Settings = settings

		if *vmDirFlag != "" {
			for _, vs := range plan.VirtualServers {
				if vs.OriginDir == *vmDirFlag {
					plan.VirtualServers = []VirtualServer{vs}
					break
				}
			}
		}

		if err := importDisks(sourceIP, plan); err != nil {
			log.Fatalf("failed to import disks: %v", err)
		}

		return
	}
}

// CreateImportPlan creates an import plan for virtual machines in a storage path like /vmfs/volumes/testdatastore
func createImportPlan(storagePath, vmName string) (ImportPlan, error) {
	storageDir, err := os.ReadDir(storagePath)
	if err != nil {
		return ImportPlan{}, err
	}

	var plan ImportPlan
	for _, item := range storageDir {
		if strings.HasPrefix(item.Name(), ".") {
			continue
		}

		if !item.IsDir() {
			continue
		}

		if vmName != "" && vmName != item.Name() {
			continue
		}

		vsPath := filepath.Join(storagePath, item.Name())

		if common.IsExists(filepath.Join(vsPath, ".skip_import")) {
			continue
		}

		vs, err := ParseVMWareVirtualServerPath(vsPath)
		if err != nil {
			return ImportPlan{}, fmt.Errorf("failed to parse virtual server path %s: %v", vsPath, err)
		}

		vs.CustomPlan = fillSolusPlanDefaults(vs.CustomPlan)

		plan.VirtualServers = append(plan.VirtualServers, vs)
	}

	return plan, nil
}

func createGuestOSoOSImageVersionID(plan ImportPlan) map[string]int {
	guestOSToOSImageVersionID := map[string]int{}
	for _, vs := range plan.VirtualServers {
		guestOSToOSImageVersionID[vs.GuestOS] = 0
	}

	return guestOSToOSImageVersionID
}
