package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	petname "github.com/dustinkirkland/golang-petname"
)

const (
	Port           = 53317
	MulticastGroup = "224.0.0.167"
	IfaceName      = "wlan0"
	DeviceModel    = "Kindle"
	DeviceType     = "mobile"
	Version        = "2.1"
	SavePath       = "/mnt/us/documents"
)

type MulticastMessage struct {
	Alias        string `json:"alias"`
	Version      string `json:"version"`
	DeviceModel  string `json:"deviceModel,omitempty"`
	DeviceType   string `json:"deviceType,omitempty"`
	Fingerprint  string `json:"fingerprint"`
	Port         int    `json:"port"`
	Protocol     string `json:"protocol"`
	Download     bool   `json:"download"`
	Announcement bool   `json:"announcement"`
}

type RegisterResponse struct {
	Alias       string `json:"alias"`
	Version     string `json:"version"`
	DeviceModel string `json:"deviceModel,omitempty"`
	DeviceType  string `json:"deviceType,omitempty"`
	Fingerprint string `json:"fingerprint"`
	Download    bool   `json:"download"`
}

type PrepareUploadFile struct {
	FileName string `json:"fileName"`
}

type PrepareUploadRequest struct {
	Files map[string]PrepareUploadFile `json:"files"`
}

var (
	sessionPending    int
	sessionMu         sync.Mutex
	sessionBusy       bool
	deviceAlias       = strings.Title(petname.Generate(2, " "))
	deviceFingerprint = petname.Generate(5, "-")
	sessionFiles      = map[string]string{}
	printLine         = -3
)

func printScreen(msg string) {
	exec.Command("/usr/bin/fbink", "-S", "4", "-y", fmt.Sprintf("%d", printLine), "-m", "-h", msg).Run() //not sure how it's gonna look on other models
	printLine++
}

func main() {
	if !acquireLock() {
		return
	}
	go startUDPListener()

	http.HandleFunc("/api/localsend/v2/register", handleRegister)
	http.HandleFunc("/api/localsend/v2/prepare-upload", handlePrepareUpload)
	http.HandleFunc("/api/localsend/v2/upload", handleUpload)

	printScreen("waiting for files...")
	if err := http.ListenAndServe(":53317", nil); err != nil {
		printScreen("error: " + err.Error())
		select {}
	}
}

func buildAnnouncement() []byte {
	msg := MulticastMessage{
		Alias:        deviceAlias,
		Version:      Version,
		DeviceModel:  DeviceModel,
		DeviceType:   DeviceType,
		Fingerprint:  deviceFingerprint,
		Port:         Port,
		Protocol:     "http",
		Download:     true,
		Announcement: true,
	}
	data, _ := json.Marshal(msg)
	return data
}

func startUDPListener() {
	iface, err := net.InterfaceByName(IfaceName)
	if err != nil {
		iface = nil
	}

	groupAddr, err := net.ResolveUDPAddr("udp4", "224.0.0.167:53317")
	if err != nil {
		return
	}

	conn, err := net.ListenMulticastUDP("udp4", iface, groupAddr)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadBuffer(65536)

	send := func(addr *net.UDPAddr) {
		conn.WriteToUDP(buildAnnouncement(), addr)
	}

	send(groupAddr)

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			send(groupAddr)
		}
	}()

	buf := make([]byte, 65536)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		var probe struct {
			Announcement bool `json:"announcement"`
		}
		if json.Unmarshal(buf[:n], &probe) == nil && probe.Announcement {
			send(remoteAddr)
		}
	}
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(RegisterResponse{
		Alias:       deviceAlias,
		Version:     Version,
		DeviceModel: DeviceModel,
		DeviceType:  DeviceType,
		Fingerprint: deviceFingerprint,
		Download:    true,
	})
}

func handlePrepareUpload(w http.ResponseWriter, r *http.Request) {
	sessionMu.Lock()
	if sessionBusy {
		sessionMu.Unlock()
		http.Error(w, `{"message":"busy"}`, http.StatusServiceUnavailable)
		return
	}
	sessionBusy = true
	sessionMu.Unlock()

	var req PrepareUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sessionMu.Lock()
		sessionBusy = false
		sessionMu.Unlock()
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	tokens := make(map[string]string, len(req.Files))
	for id, f := range req.Files {
		tokens[id] = id
		sessionFiles[id] = f.FileName
	}

	sessionMu.Lock()
	sessionPending = len(req.Files)
	sessionMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessionId": "kindle-session-999",
		"files":     tokens,
	})
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	fileId := r.URL.Query().Get("fileId")
	fileName := sessionFiles[fileId]
	isNameTaken := fileExists(SavePath + "/" + fileName)
	if isNameTaken {
		fileName = fixNameConflict(SavePath + "/" + fileName)
		parts := strings.Split(fileName, "/")
		fileName = parts[len(parts)-1]
	}
	if fileName == "" {
		fileName = r.URL.Query().Get("fileName")
	}
	if fileName == "" {
		fileName = "received_file"
	}

	printScreen("receiving " + fileName)

	if err := os.MkdirAll(SavePath, 0755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out, err := os.Create(SavePath + "/" + fileName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer out.Close()

	if _, err = io.Copy(out, r.Body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sessionMu.Lock()
	sessionPending--
	if sessionPending <= 0 {
		sessionBusy = false
		sessionFiles = map[string]string{}
	}
	sessionMu.Unlock()

	printScreen("saved " + fileName)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"success"}`))
}

func fileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}

func fixNameConflict(filePath string) string {
	ext := ""
	if idx := strings.LastIndex(filePath, "."); idx != -1 {
		ext = filePath[idx:]
		filePath = filePath[:idx]
	}

	for i := 1; ; i++ {
		newPath := fmt.Sprintf("%s(%d)%s", filePath, i, ext)
		if !fileExists(newPath) {
			return newPath
		}
	}
}

func acquireLock() bool {
	lockFile := "/tmp/localsend-lock.lock"
	f, err := os.OpenFile(lockFile, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		return false
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		printScreen("server is already running")
		return false
	}

	return true
}
