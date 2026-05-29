package inventory

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ScanApplications detects installed software applications on the host system.
// Runs completely in user-space without administrative/root privileges.
func ScanApplications() []string {
	var apps []string
	switch runtime.GOOS {
	case "linux":
		apps = scanLinuxPackages()
	case "windows":
		apps = scanWindowsRegistry()
	case "darwin":
		apps = scanMacApplications()
	}

	// Always fallback to scanning active running common processes
	apps = mergeUnique(apps, scanCommonRunningServices())
	
	if len(apps) == 0 {
		return []string{"system-core-shell"}
	}
	return apps
}

func scanLinuxPackages() []string {
	var detected []string

	// 1. Direct parsing of Debian Package Database (dpkg)
	// This is standard-library only, avoids exec overhead, and runs in unprivileged user-space
	dpkgPath := "/var/lib/dpkg/status"
	file, err := os.Open(dpkgPath)
	if err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "Package: ") {
				pkgName := strings.TrimPrefix(line, "Package: ")
				// Capture common enterprise server services
				if isTargetApp(pkgName) {
					detected = append(detected, pkgName)
				}
			}
		}
	}

	// 2. Check common paths if package manager DB is inaccessible
	checkPaths := []string{
		"/usr/sbin/nginx",
		"/usr/bin/docker",
		"/usr/bin/mysql",
		"/usr/bin/postgres",
		"/usr/bin/redis-server",
	}
	for _, path := range checkPaths {
		if _, err := os.Stat(path); err == nil {
			detected = append(detected, filepath.Base(path))
		}
	}

	return detected
}

func scanWindowsRegistry() []string {
	var detected []string

	// Run reg query to get all entries under HKLM Uninstall (32-bit & 64-bit views)
	// This runs completely in standard user-space without UAC elevation!
	paths := []string{
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
		`HKLM\SOFTWARE\Wow6432Node\Microsoft\Windows\CurrentVersion\Uninstall`,
	}

	for _, registryPath := range paths {
		cmd := exec.Command("reg", "query", registryPath, "/s")
		var out bytes.Buffer
		cmd.Stdout = &out
		err := cmd.Run()
		if err != nil {
			continue
		}

		// Parse the standard command output for DisplayName records
		scanner := bufio.NewScanner(&out)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "DisplayName") {
				parts := strings.SplitN(line, "REG_SZ", 2)
				if len(parts) == 2 {
					appName := strings.TrimSpace(parts[1])
					if isTargetApp(appName) || len(detected) < 15 { // Store first 15 for visibility
						detected = append(detected, appName)
					}
				}
			}
		}
	}

	return detected
}

func scanMacApplications() []string {
	var detected []string

	// Scan primary Applications directory
	appsDir := "/Applications"
	files, err := os.ReadDir(appsDir)
	if err == nil {
		for _, file := range files {
			if strings.HasSuffix(file.Name(), ".app") {
				appName := strings.TrimSuffix(file.Name(), ".app")
				if isTargetApp(appName) || len(detected) < 15 {
					detected = append(detected, appName)
				}
			}
		}
	}

	return detected
}

// scanCommonRunningServices scans active process namespace names to identify running core programs
func scanCommonRunningServices() []string {
	var detected []string
	
	// We check for common running executables (e.g. docker, nginx, postgres, mysql, java, node)
	services := []string{"nginx", "postgres", "mysql", "docker", "redis", "node", "java", "httpd", "sshd"}
	
	switch runtime.GOOS {
	case "linux", "darwin":
		cmd := exec.Command("ps", "-axco", "command")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			scanner := bufio.NewScanner(&out)
			for scanner.Scan() {
				procName := strings.ToLower(strings.TrimSpace(scanner.Text()))
				for _, s := range services {
					if procName == s {
						detected = append(detected, s)
					}
				}
			}
		}
	case "windows":
		cmd := exec.Command("tasklist", "/nh", "/fo", "csv")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			scanner := bufio.NewScanner(&out)
			for scanner.Scan() {
				line := strings.ToLower(scanner.Text())
				for _, s := range services {
					if strings.Contains(line, s) {
						detected = append(detected, s)
					}
				}
			}
		}
	}

	return uniqueSlice(detected)
}

func isTargetApp(name string) bool {
	name = strings.ToLower(name)
	targets := []string{
		"nginx", "apache", "httpd", "mysql", "postgres", "redis", "docker", 
		"kubernetes", "splunk", "elastic", "java", "node", "python", "active-directory",
	}
	for _, target := range targets {
		if strings.Contains(name, target) {
			return true
		}
	}
	return false
}

func mergeUnique(a, b []string) []string {
	m := make(map[string]bool)
	for _, item := range a {
		m[item] = true
	}
	for _, item := range b {
		m[item] = true
	}
	var res []string
	for k := range m {
		res = append(res, k)
	}
	return res
}

func uniqueSlice(s []string) []string {
	m := make(map[string]bool)
	var res []string
	for _, item := range s {
		if !m[item] {
			m[item] = true
			res = append(res, item)
		}
	}
	return res
}
