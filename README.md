Import from VMWare ESXi to SolusVM 2
= 

Using this tool you can import virtual servers from VMWare ESXi host to a compute resource managed by SolusVM 2.

## Known Issues

1. Import of a running virtual machine will fail with the "nbdkit: ssh[1]: error: cannot open file for reading: SFTP server: Failure" error.
2. Import of a virtual machine with IDE disk controller will fail.
3. Import of a virtual machine with a directory with the space character (" ") will fail. 
4. Importing of a virtual machine with a large disk may fail because of unstable network connection.
5. If OS of a virtual server is older than OS of a SolusVM 2 compute resource then import will fail with `libguestfs error: file_architecture: unknown architecture: /usr/lib/modules/6.8.0-31-generic` or `no installed kernel packages were found`.
6. If Windows virtual machine was not stopped gracefully the following error will occur: `virt-v2v: error: filesystem was mounted read-only, even though we asked for it to be mounted read-write.  This usually means that the filesystem was not cleanly unmounted.  Possible causes include trying to convert a guest which is running, or using Windows Hibernation or Fast Restart`.
7. After the import, Windows virtual server will be using `sata` disk driver which is not optimal, but it is only to allow the first boot. On the first boot, VirtIO drivers will be automatically installed inside the guest OS. Then you have to shutdown the virtual server and change disk driver to `scsi`.
8. If Windows virtual server can't boot with the "Inaccessible boot device" error, try to change "Disk Driver" setting to `sata` or `virtio`. Install VirtIO drivers inside Windows using VirtIO ISO for Windows, then stop virtual server and change "Disk Driver" setting back to `scsi`. It's highly recommended to run virtual server with `scsi` disk driver.

## Prerequisites

1. Install SolusVM 2 and add a new compute resource where you will import virtual servers from VMWare ESXi host. Pay attention that OS version of a compute resource has to be newer or equal to the newest OS version of imported server, otherwise the disk import will fail. **CentOS 9 Stream is highly recommended** for the import task. You don't need to use hardware server as CR - you can use virtual machine created in SolusVM 2 instead. After the initial import you can migrate imported servers to any other compute resource in your SolusVM 2 cluster.
2. Enable SSH service in VMWare console.
3. Put public SSH key for `root` user to `/etc/ssh/keys-root/authorized_keys`
4. Check SSH authorisation by public key is working from a compute resource to VMWare ESXi host.
5. Enable execution of non-installed binaries on the VMWare ESXi host:
```shell
esxcli system settings advanced set -o /User/execInstalledOnly -i 0
```

## Import

1. In **SolusVM 2 Admin interface > Access > API Tokens > Generate API Token** create a token, copy and save it somewhere.

2. Login to your SolusVM 2 Compute Resource for import over SSH.

3. Upload vmware-import binary to a compute resource:

4. Install packages on a compute resource: 
```shell
dnf install virt-v2v nbdkit edk2-ovmf
```
or
```shell
apt install virt-v2v nbdkit libnbd-bin ovmf
```

5. Create an example of settings file: 
```shell
./vmware-importer -create-settings-file -settings-file-path settings.json
```

6. Fill `settings.json` file: 

`source_ip` - IP address of VMWare ESXi host.

`api_url` - link to API endpoint of your SolusVM 2 managment node, replace solus.example.tld with your actual domain.

`api_token` - token from step 1.

`guest_os_to_os_image_version_id` - IDs of images in **SolusVM 2 Admin interface > Images > Operating Systems**.

`user_id` - ID of administrator who will be the owner of imported virtual machines.

`project_id` - ID of project of the owner. Can be found by opening https://your.solusvm2.domain under administrator - owner of imported virtual servers.

`compute_resource_id` - ID of compute resource where imported virtual servers will be located. Can be found in **SolusVM 2 Admin interface > Compute Resources**.

`location_id` - ID of location of compute resource where imported virtual servers will be located. Can be found in **SolusVM 2 Admin interface > Compute Resources > Locations**.

`ssh_key` - optional field.

`additional_disk_offer_id` - optional field. Can be used if virtual server in VMWare has additional disk. Can be found in **SolusVM 2 Admin interface > Compute Resources > Offers**.

Example of the file:
```json
{
  "source_ip": "192.168.192.168",
  "api_url": "https://solus.example.tld/api/v1/",
  "api_token": "eyJ0eXAiOiJKV...sYFo",
  "defaults": {
    "guest_os_to_os_image_version_id": {
      "centos6-64": 1,
      "centos7-64": 2,
      "ubuntu-64": 17,
      "almalinux-64": 18,
      "debian12-64": 50,
      "windows9srv-64": 25,
      "windows2019srv-64": 25,
      "windows2019srvNext-64": 55
    },
    "user_id": 1,
    "project_id": 1,
    "compute_resource_id": 262,
    "location_id": 3,
    "ssh_keys": [],
    "additional_disk_offer_id": 1
  }
}
```

7. Create import plan - it will create plans for all virtual servers on VMWare host:
```shell
./vmware-importer -source-ip 192.168.192.168 -private-key ~/.ssh/id_rsa \ 
                   -create-import-plan \
                   -import-plan-file-path import_plan.json \
                   -storage-path /vmfs/volumes/datastore1 \
```

It is possible to create plan (and import) only one specific virtual servers with option `-vm-dir`:

```shell
./vmware-importer -source-ip 192.168.192.168 -private-key ~/.ssh/id_rsa \ 
                   -create-import-plan \
                   -import-plan-file-path import_plan.json \
                   -storage-path /vmfs/volumes/datastore1 \
                   -vm-dir "win2k35"
```

8. Create virtual servers in SolusVM 2 by import plan:
```shell
./vmware-importer -create-virtual-servers-by-import-plan -import-plan-file-path import_plan.json
```
The command will create new virtual servers in SolusVM 2 using API credentials and defaults you specified in settings file.

9. Import disks from VMWare ESXi host:
```shell
./vmware-importer -source-ip 192.168.192.168 -private-key ~/.ssh/id_rsa -import-plan-file-path import_plan.json  -import-disks
```
If automatic import could be performed you can try import disks manually:
```shell
virt-v2v \
 -i vmx -it ssh \
 "ssh://root@192.168.192.168/vmfs/volumes/datastore1/win2k35/win2k35.vmx" \
 -o local -os /var/lib/libvirt/images/123/
```
