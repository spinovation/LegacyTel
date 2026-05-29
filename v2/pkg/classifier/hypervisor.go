package classifier

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// DetectHypervisor programmatically determines if the host is virtualized
// and classifies the hypervisor into Type 1 (Bare-Metal) or Type 2 (Hosted).
func DetectHypervisor() (hvType string, hvName string) {
	switch runtime.GOOS {
	case "linux":
		return detectLinuxHypervisor()
	case "windows":
		return detectWindowsHypervisor()
	case "darwin":
		return detectMacHypervisor()
	default:
		return "none", "Physical Hardware"
	}
}

func detectLinuxHypervisor() (string, string) {
	// 1. Check DMI Product Name
	productBytes, err := readSysFile("/sys/class/dmi/id/product_name")
	vendorBytes, _ := readSysFile("/sys/class/dmi/id/sys_vendor")
	
	product := strings.ToLower(string(productBytes))
	vendor := strings.ToLower(string(vendorBytes))

	if err == nil {
		if strings.Contains(product, "virtualbox") {
			return "type-2", "Oracle VirtualBox"
		}
		if strings.Contains(product, "vmware") {
			// ESXi is Type 1, Workstation is Type 2. 
			// Inside the VM, we look at the vendor or system info.
			// ESXi usually reports "VMware Virtual Platform" or "ESXi".
			if strings.Contains(product, "workstation") || strings.Contains(product, "player") {
				return "type-2", "VMware Workstation"
			}
			return "type-1", "VMware ESXi"
		}
		if strings.Contains(product, "hyper-v") || strings.Contains(product, "virtual machine") {
			return "type-1", "Microsoft Hyper-V"
		}
		if strings.Contains(product, "kvm") || strings.Contains(product, "qemu") {
			return "type-1", "KVM/QEMU"
		}
		if strings.Contains(product, "xen") {
			return "type-1", "Xen Hypervisor"
		}
	}

	// 2. Check /sys/hypervisor/type fallback
	hypTypeBytes, err := readSysFile("/sys/hypervisor/type")
	if err == nil {
		hypType := strings.TrimSpace(strings.ToLower(string(hypTypeBytes)))
		if hypType == "xen" {
			return "type-1", "Xen Hypervisor"
		}
		if hypType == "kvm" {
			return "type-1", "KVM/QEMU"
		}
	}

	// 3. Fallback DMI Vendor checks
	if strings.Contains(vendor, "qemu") || strings.Contains(vendor, "red hat") {
		return "type-1", "KVM/QEMU"
	}
	if strings.Contains(vendor, "innotek") {
		return "type-2", "Oracle VirtualBox"
	}

	return "none", "Physical Hardware"
}

func detectWindowsHypervisor() (string, string) {
	// Run an unprivileged command line query to read registry system details
	// This avoids loading complex DLL packages or unsafe standard integrations
	cmd := exec.Command("reg", "query", "HKLM\\SYSTEM\\CurrentControlSet\\Control\\SystemInformation", "/v", "SystemProductName")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()

	product := ""
	if err == nil {
		product = strings.ToLower(out.String())
	}

	cmdVendor := exec.Command("reg", "query", "HKLM\\SYSTEM\\CurrentControlSet\\Control\\SystemInformation", "/v", "SystemManufacturer")
	var outVendor bytes.Buffer
	cmdVendor.Stdout = &outVendor
	_ = cmdVendor.Run()
	vendor := strings.ToLower(outVendor.String())

	if strings.Contains(product, "virtualbox") {
		return "type-2", "Oracle VirtualBox"
	}
	if strings.Contains(product, "vmware") {
		if strings.Contains(product, "workstation") || strings.Contains(product, "player") {
			return "type-2", "VMware Workstation"
		}
		return "type-1", "VMware ESXi"
	}
	if strings.Contains(product, "virtual machine") || strings.Contains(vendor, "microsoft") {
		return "type-1", "Microsoft Hyper-V"
	}
	if strings.Contains(product, "kvm") || strings.Contains(product, "qemu") {
		return "type-1", "KVM/QEMU"
	}
	if strings.Contains(product, "xen") {
		return "type-1", "Xen Hypervisor"
	}

	return "none", "Physical Hardware"
}

func detectMacHypervisor() (string, string) {
	// On macOS development boxes, we run inside local hypervisors (Type 2 hosted)
	// We check for running hypervisor client drivers or standard system parameters
	cmd := exec.Command("sysctl", "-n", "machdep.cpu.brand_string")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()

	brand := strings.ToLower(out.String())
	if err == nil {
		if strings.Contains(brand, "virtualbox") || strings.Contains(brand, "vbox") {
			return "type-2", "Oracle VirtualBox"
		}
	}

	// Fallback to checking active application folders or standard drivers
	if _, err := os.Stat("/Applications/VirtualBox.app"); err == nil {
		return "type-2", "Oracle VirtualBox (Detected locally)"
	}
	if _, err := os.Stat("/Applications/VMware Fusion.app"); err == nil {
		return "type-2", "VMware Fusion"
	}
	if _, err := os.Stat("/Applications/Parallels Desktop.app"); err == nil {
		return "type-2", "Parallels Desktop"
	}

	return "none", "macOS Core Hardware"
}

func readSysFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
