package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
)

type InstalledSoftware struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type RunningProcess struct {
	PID        int32   `json:"pid"`
	Name       string  `json:"name"`
	CPUPercent float64 `json:"cpuPercent"`
	RAMMB      float32 `json:"ramMb"`
}

type BrowserTab struct {
	Browser string `json:"browser"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Domain  string `json:"domain"`
}

type MachineData struct {
	Enterprise  string `json:"enterprise"`
	Responsible string `json:"responsible"`
	Department  string `json:"department"`
	Hostname    string `json:"hostname"`
	Branch      string `json:"branch"`
	Serial      string `json:"serial"`
	MachineType string `json:"machineType"`
	Processor   string `json:"processor"`
	Memory      string `json:"memory"`
	Storage     string `json:"storage"`
	StorageType string `json:"storageType"`
	OS          string `json:"os"`
	Guarantee   string `json:"guarantee"`
	Status      string `json:"status"`
	Connected   bool   `json:"connected"`

	DateGuarantee   string `json:"dateGuarantee"`
	DateAcquisition string `json:"dateAcquisition"`

	Softwares []InstalledSoftware `json:"softwares"`
	Processes []RunningProcess    `json:"processes"`
	Tabs      []BrowserTab        `json:"tabs"`
}

func getInstalledSoftware() []InstalledSoftware {

	cmd := exec.Command(
		"powershell",
		`Get-ItemProperty HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\* |
		Select-Object DisplayName, DisplayVersion |
		ConvertTo-Json`,
	)

	output, err := cmd.Output()

	if err != nil {
		fmt.Println("Erro softwares:", err)
		return nil
	}

	var raw []map[string]interface{}

	err = json.Unmarshal(output, &raw)

	if err != nil {
		fmt.Println("Erro JSON softwares:", err)
		return nil
	}

	var softwares []InstalledSoftware

	for _, item := range raw {

		name, ok := item["DisplayName"].(string)

		if !ok || name == "" {
			continue
		}

		version := ""

		if v, ok := item["DisplayVersion"].(string); ok {
			version = v
		}

		softwares = append(softwares, InstalledSoftware{
			Name:    name,
			Version: version,
		})
	}

	return softwares
}

func getProcesses() []RunningProcess {

	procs, err := process.Processes()

	if err != nil {
		return nil
	}

	var result []RunningProcess

	for _, p := range procs {

		name, _ := p.Name()

		cpuPercent, _ := p.CPUPercent()

		memInfo, _ := p.MemoryInfo()

		var ramMB float32

		if memInfo != nil {
			ramMB = float32(memInfo.RSS) / 1024 / 1024
		}

		result = append(result, RunningProcess{
			PID:        p.Pid,
			Name:       name,
			CPUPercent: cpuPercent,
			RAMMB:      ramMB,
		})
	}

	return result
}

func getChromeTabs() []BrowserTab {

	debugPorts := []string{
		"9222",
		"9223",
		"9224",
	}

	var result []BrowserTab

	for _, port := range debugPorts {

		urlDebug := fmt.Sprintf("http://127.0.0.1:%s/json", port)

		resp, err := http.Get(urlDebug)

		if err != nil {
			continue
		}

		defer resp.Body.Close()

		var tabs []map[string]interface{}

		err = json.NewDecoder(resp.Body).Decode(&tabs)

		if err != nil {
			continue
		}

		for _, tab := range tabs {

			title, _ := tab["title"].(string)
			urlTab, _ := tab["url"].(string)

			if urlTab == "" {
				continue
			}

			// Ignora páginas internas
			if strings.HasPrefix(urlTab, "chrome://") ||
				strings.HasPrefix(urlTab, "devtools://") ||
				strings.HasPrefix(urlTab, "edge://") ||
				strings.HasPrefix(urlTab, "brave://") {
				continue
			}

			domain := extractDomain(urlTab)

			browser := detectBrowser(urlTab)

			result = append(result, BrowserTab{
				Browser: browser,
				Title:   title,
				URL:     urlTab,
				Domain:  domain,
			})
		}
	}

	return result
}

func detectBrowser(url string) string {

	if strings.Contains(strings.ToLower(url), "brave") {
		return "Brave"
	}

	return "Chrome"
}

func extractDomain(rawURL string) string {

	parsed, err := url.Parse(rawURL)

	if err != nil {
		return ""
	}

	domain := parsed.Host

	domain = strings.Replace(domain, "www.", "", 1)

	return domain
}

func getDiskType() string {

	if runtime.GOOS == "windows" {

		cmd := exec.Command(
			"powershell",
			"Get-PhysicalDisk | Select-Object MediaType",
		)

		output, err := cmd.Output()

		if err != nil {
			return "Desconhecido"
		}

		outStr := string(output)

		if strings.Contains(outStr, "SSD") {
			return "SSD"
		}

		if strings.Contains(outStr, "HDD") {
			return "HDD"
		}
	}

	return "Desconhecido"
}

func getSerialNumber() string {

	cmd := exec.Command(
		"powershell",
		"(Get-WmiObject Win32_BIOS).SerialNumber",
	)

	output, err := cmd.Output()

	if err != nil {
		return "UNKNOWN"
	}

	return strings.TrimSpace(string(output))
}

func sendToBackend(data MachineData) {

	jsonData, err := json.MarshalIndent(data, "", "  ")

	if err != nil {
		fmt.Println("Erro ao converter JSON:", err)
		return
	}

	fmt.Println("\n===== JSON ENVIADO =====")
	fmt.Println(string(jsonData))

	resp, err := http.Post(
		"http://127.0.0.1:8080/api/agent/data",
		"application/json",
		bytes.NewBuffer(jsonData),
	)

	if err != nil {
		fmt.Println("Erro ao enviar:", err)
		return
	}

	defer resp.Body.Close()

	fmt.Println("Enviado! Status:", resp.Status)
}

func main() {

	for {

		timestamp := time.Now().Format(time.RFC3339)

		hostInfo, _ := host.Info()
		memInfo, _ := mem.VirtualMemory()
		diskInfo, _ := disk.Usage("/")
		cpuInfo, _ := cpu.Info()

		diskType := getDiskType()
		serial := getSerialNumber()

		softwares := getInstalledSoftware()
		processes := getProcesses()
		tabs := getChromeTabs()

		fmt.Println("\n===== DADOS DA MÁQUINA =====")

		fmt.Println("Hostname:", hostInfo.Hostname)
		fmt.Println("Sistema:", hostInfo.Platform)

		fmt.Println("RAM Total:",
			memInfo.Total/1024/1024,
			"MB",
		)

		fmt.Println("RAM Usada:",
			memInfo.Used/1024/1024,
			"MB",
		)

		fmt.Println("Disco Total:",
			diskInfo.Total/1024/1024/1024,
			"GB",
		)

		fmt.Println("Disco Usado:",
			diskInfo.Used/1024/1024/1024,
			"GB",
		)

		fmt.Println("Data/Hora:", timestamp)

		var processador string

		if len(cpuInfo) > 0 {

			processador = cpuInfo[0].ModelName

			fmt.Println("Processador:", processador)
		}

		fmt.Println("Tipo de Armazenamento:", diskType)

		// =========================
		// SOFTWARES
		// =========================

		fmt.Println("\n===== SOFTWARES =====")

		for _, software := range softwares {

			fmt.Println("Nome:", software.Name)
			fmt.Println("Versão:", software.Version)
			fmt.Println("-------------------")
		}

		// =========================
		// PROCESSOS
		// =========================

		fmt.Println("\n===== PROCESSOS =====")

		for _, proc := range processes {

			fmt.Println("PID:", proc.PID)
			fmt.Println("Nome:", proc.Name)
			fmt.Println("CPU:", proc.CPUPercent)
			fmt.Println("RAM MB:", proc.RAMMB)

			fmt.Println("-------------------")
		}

		// =========================
		// TABS
		// =========================

		fmt.Println("\n===== TABS =====")

		for _, tab := range tabs {

			fmt.Println("Browser:", tab.Browser)
			fmt.Println("Título:", tab.Title)
			fmt.Println("URL:", tab.URL)
			fmt.Println("Domain:", tab.Domain)

			fmt.Println("-------------------")
		}

		data := MachineData{
			Hostname:    hostInfo.Hostname,
			Processor:   processador,
			Memory:      fmt.Sprintf("%d", memInfo.Total/1024/1024),
			Storage:     fmt.Sprintf("%d", diskInfo.Total/1024/1024/1024),
			Serial:      serial,
			StorageType: diskType,
			OS:          hostInfo.Platform,
			Connected:   true,
			Status:      "ACTIVE",

			Softwares: softwares,
			Processes: processes,
			Tabs:      tabs,
		}

		sendToBackend(data)

		fmt.Println("\n============================")

		time.Sleep(30 * time.Second)
	}
}
