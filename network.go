package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"path/filepath"

	"github.com/cretz/bine/tor"
)

 
type NetworkManager struct {
	TorInstance *tor.Tor
	OnionAddr   string
	TorDataDir  string  

	 
	Connections      map[string]net.Conn  
	Incoming         chan string          
	Outgoing         chan string          
	ErrorChan        chan error
	SessionKeys      map[string][]byte  
	PeerNames        map[string]string  
	PeerColors       map[string]string  
	broadcastRunning bool               
	mu               sync.Mutex
}

func NewNetworkManager() *NetworkManager {
	return &NetworkManager{
		Incoming:    make(chan string, 10),
		Outgoing:    make(chan string, 10),
		ErrorChan:   make(chan error, 1),
		Connections: make(map[string]net.Conn),
		SessionKeys: make(map[string][]byte),
		PeerNames:   make(map[string]string),
		PeerColors:  make(map[string]string),
	}
}

 
func (nm *NetworkManager) StartTor(ctx context.Context, progress chan<- string) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	 
	if nm.TorInstance != nil {
		 
		return nil
	}

	if progress != nil {
		progress <- "Checking for tor.exe..."
	}

	 
	var exePath string
	if _, err := os.Stat("tor.exe"); err == nil {
		absPath, err := filepath.Abs("tor.exe")
		if err == nil {
			exePath = absPath
		} else {
			exePath = "tor.exe"  
		}
	}

	if progress != nil {
		progress <- fmt.Sprintf("Using Tor at: %s", exePath)
	}

	 
	cwd, _ := os.Getwd()
	geoipPath := filepath.Join(cwd, "geoip")
	geoip6Path := filepath.Join(cwd, "geoip6")

	 
	 
	nm.TorDataDir = fmt.Sprintf("tor-data-%d", os.Getpid())

	 
	conf := &tor.StartConf{
		ExePath:     exePath,
		DataDir:     nm.TorDataDir,
		DebugWriter: &logWriter{ch: progress},  
		ExtraArgs: []string{
			"--GeoIPFile", geoipPath,
			"--GeoIPv6File", geoip6Path,
		},
	}

	 
	 

	t, err := tor.Start(ctx, conf)
	if err != nil {
		return fmt.Errorf("failed to start tor (path: %s): %v.\nCheck directory permissions or antivirus.", exePath, err)
	}
	nm.TorInstance = t

	if progress != nil {
		progress <- "Tor started, bootstrapping..."
	}
	return nil
}

 
func (nm *NetworkManager) Host(ctx context.Context, keyPath string, secret string, masterKey []byte, progress chan<- string) error {
	if progress != nil {
		progress <- "Loading private key..."
	}

	privKey, err := loadOrGenerateKey(keyPath, masterKey)
	if err != nil {
		return fmt.Errorf("failed to load/gen key: %v", err)
	}

	if progress != nil {
		progress <- "Setting up Onion Service..."
	}

	 
	listenCtx, listenCancel := context.WithTimeout(ctx, 3*time.Minute)
	defer listenCancel()

	onion, err := nm.TorInstance.Listen(listenCtx, &tor.ListenConf{
		Key:         privKey,
		RemotePorts: []int{80},
		Version3:    true,
	})
	if err != nil {
		return fmt.Errorf("failed to create onion service: %v", err)
	}
	nm.OnionAddr = onion.ID  

	if progress != nil {
		progress <- fmt.Sprintf("Listening on %s.onion", nm.OnionAddr)
	}

	 
	go nm.acceptLoop(onion, secret, progress)

	return nil
}

 
func (nm *NetworkManager) acceptLoop(listener net.Listener, secret string, progress chan<- string) {
	peerCount := 0

	for {
		if progress != nil && peerCount == 0 {
			progress <- "Waiting for peer..."
		}

		conn, err := listener.Accept()
		if err != nil {
			 
			return
		}

		peerCount++
		go nm.handlePeer(conn, secret, progress, peerCount)
	}
}

 
func (nm *NetworkManager) handlePeer(conn net.Conn, secret string, progress chan<- string, peerNum int) {
	if progress != nil {
		progress <- fmt.Sprintf("Peer %d connecting... Handshake...", peerNum)
	}

	 
	sessionKey, peerName, peerColor, err := nm.performHandshakeHost(conn, secret)
	if err != nil {
		nm.ErrorChan <- fmt.Errorf("handshake failed: %v", err)
		conn.Close()
		return
	}

	 
	peerID := fmt.Sprintf("peer_%d_%x", peerNum, sha256.Sum256([]byte(conn.RemoteAddr().String())))[:16]

	 
	nm.mu.Lock()
	nm.Connections[peerID] = conn
	nm.SessionKeys[peerID] = sessionKey
	nm.PeerNames[peerID] = peerName
	nm.PeerColors[peerID] = peerColor
	nm.mu.Unlock()

	 
	if len(nm.Connections) == 1 {
		if progress != nil {
			progress <- "You are not alone."
		}
	} else {
		if progress != nil {
			progress <- fmt.Sprintf("%s joined us.", peerName)
		}
	}

	 
	if len(nm.Connections) > 5 {
		if progress != nil {
			progress <- "Too many will slow us down."
		}
	}

	 
	nm.startPeerIO(peerID, conn, sessionKey, peerName, peerColor)
}

 
func (nm *NetworkManager) Connect(ctx context.Context, address string, secret string, progress chan<- string) error {
	if progress != nil {
		progress <- fmt.Sprintf("Connecting to %s...", address)
	}

	dialer, err := nm.TorInstance.Dialer(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to get dialer: %v", err)
	}

	conn, err := dialer.Dial("tcp", address+":80")
	if err != nil {
		return fmt.Errorf("connect failed: %v", err)
	}

	if progress != nil {
		progress <- "Connected. Verifying secret..."
	}

	 
	if err := nm.performHandshakeClient(conn, secret); err != nil {
		conn.Close()
		return fmt.Errorf("handshake failed: %v", err)
	}

	if progress != nil {
		progress <- "You are not alone."
	}

	 
	peerID := "host"
	sessionKey := DeriveSessionKey(secret)

	 
	nm.mu.Lock()
	nm.Connections[peerID] = conn
	nm.SessionKeys[peerID] = sessionKey
	nm.PeerNames[peerID] = "Host"
	nm.PeerColors[peerID] = "45"
	nm.mu.Unlock()

	 
	nm.startPeerIO(peerID, conn, sessionKey, "Host", "45")

	return nil
}

 
 
func (nm *NetworkManager) CheckConnection(address string) bool {
	 
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if nm.TorInstance == nil {
		return false
	}

	dialer, err := nm.TorInstance.Dialer(ctx, nil)
	if err != nil {
		return false
	}

	 
	conn, err := dialer.Dial("tcp", address+":80")
	if err != nil {
		return false
	}
	defer conn.Close()

	 
	return true
}

func (nm *NetworkManager) performHandshakeHost(conn net.Conn, secret string) ([]byte, string, string, error) {
	 
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	defer conn.SetReadDeadline(time.Time{})  

	reader := bufio.NewReader(conn)
	hashStr, err := reader.ReadString('\n')
	if err != nil {
		return nil, "", "", fmt.Errorf("read secret failed: %w", err)
	}
	hashStr = strings.TrimSpace(hashStr)

	expected := sha256Hash(secret)
	 
	if !strings.EqualFold(hashStr, expected) {
		return nil, "", "", fmt.Errorf("invalid secret")
	}

	 
	sessionKey := DeriveSessionKey(secret)

	 
	 
	return sessionKey, "anonymous", "240", nil
}

func (nm *NetworkManager) performHandshakeClient(conn net.Conn, secret string) error {
	conn.SetWriteDeadline(time.Now().Add(60 * time.Second))
	defer conn.SetWriteDeadline(time.Time{})

	hash := sha256Hash(secret)
	_, err := fmt.Fprintf(conn, "%s\n", hash)
	if err != nil {
		return err
	}

	 
	 
	return nil
}

func sha256Hash(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

 
func (nm *NetworkManager) startPeerIO(peerID string, conn net.Conn, sessionKey []byte, peerName string, peerColor string) {
	 
	go func() {
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			text := scanner.Text()
			 
			if len(sessionKey) > 0 {
				decrypted, err := DecryptMessage(text, sessionKey)
				if err != nil {
					nm.Incoming <- fmt.Sprintf("[Decryption Error] %v", err)
					continue
				}
				text = decrypted
			}

			 
			 
			 
			var chatMsg struct {
				Name  string `json:"name"`
				Color string `json:"color"`
			}
			if json.Unmarshal([]byte(text), &chatMsg) == nil && chatMsg.Name != "" {
				nm.mu.Lock()
				if nm.PeerNames[peerID] == "anonymous" || nm.PeerNames[peerID] == "Host" {
					nm.PeerNames[peerID] = chatMsg.Name
					nm.PeerColors[peerID] = chatMsg.Color
					peerName = chatMsg.Name
					peerColor = chatMsg.Color
				}
				nm.mu.Unlock()
			}
			nm.Incoming <- fmt.Sprintf("PEER_MSG:%s:%s:%s:%s", peerID, peerName, peerColor, text)
		}

		 
		nm.removePeer(peerID, peerName)

		if err := scanner.Err(); err != nil {
			nm.ErrorChan <- fmt.Errorf("peer %s disconnected: %v", peerName, err)
		}
	}()

	 
	nm.mu.Lock()
	if !nm.broadcastRunning {
		nm.broadcastRunning = true
		nm.mu.Unlock()
		go nm.broadcastHandler()
	} else {
		nm.mu.Unlock()
	}
}

 
func (nm *NetworkManager) broadcastHandler() {
	for msg := range nm.Outgoing {
		 
		excludePeerID := ""
		actualMsg := msg
		if strings.HasPrefix(msg, "RELAY:") {
			parts := strings.SplitN(msg, ":", 3)
			if len(parts) == 3 {
				excludePeerID = parts[1]
				actualMsg = parts[2]
			}
		}

		nm.mu.Lock()
		conns := make(map[string]net.Conn)
		keys := make(map[string][]byte)
		names := make(map[string]string)
		for id := range nm.Connections {
			conns[id] = nm.Connections[id]
			keys[id] = nm.SessionKeys[id]
			names[id] = nm.PeerNames[id]
		}
		nm.mu.Unlock()

		for peerID, conn := range conns {
			 
			if peerID == excludePeerID {
				continue
			}

			text := actualMsg
			if len(keys[peerID]) > 0 {
				encrypted, err := EncryptMessage(text, keys[peerID])
				if err != nil {
					nm.ErrorChan <- fmt.Errorf("encrypt failed: %v", err)
					continue
				}
				text = encrypted
			}
			fmt.Fprintf(conn, "%s\n", text)
		}
	}
}

 
func (nm *NetworkManager) removePeer(peerID string, peerName string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if conn, exists := nm.Connections[peerID]; exists {
		conn.Close()
		delete(nm.Connections, peerID)
		delete(nm.SessionKeys, peerID)
		delete(nm.PeerNames, peerID)
		delete(nm.PeerColors, peerID)

		 
		nm.Incoming <- fmt.Sprintf("PEER_LEFT:%s:%s", peerID, peerName)
	}
}

func (nm *NetworkManager) Close() {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	 
	for peerID, conn := range nm.Connections {
		if conn != nil {
			conn.Close()
		}
		delete(nm.Connections, peerID)
	}

	 
	nm.SessionKeys = make(map[string][]byte)
	nm.PeerNames = make(map[string]string)
	nm.PeerColors = make(map[string]string)

	 
	if nm.TorInstance != nil {
		nm.TorInstance.Close()
		nm.TorInstance = nil  
	}

	 
	if nm.TorDataDir != "" {
		 
		time.Sleep(100 * time.Millisecond)
		os.RemoveAll(nm.TorDataDir)
		nm.TorDataDir = ""
	}

	 
	 
	 
	 

	 
	nm.OnionAddr = ""
}

 
func loadOrGenerateKey(path string, masterKey []byte) (ed25519.PrivateKey, error) {
	 
	data, err := os.ReadFile(path)
	if err == nil {
		decryptedData := data
		wasEncrypted := false

		 
		if len(masterKey) > 0 {
			decrypted, err := DecryptBytes(data, masterKey)
			if err == nil {
				decryptedData = decrypted
				wasEncrypted = true
			}
			 
			 
		}

		block, _ := pem.Decode(decryptedData)
		if block != nil {
			if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
				if edKey, ok := key.(ed25519.PrivateKey); ok {
					 
					 
					if len(masterKey) > 0 && !wasEncrypted {
						 
						saveKey(path, edKey, masterKey)
					}
					return edKey, nil
				}
			}
		}
	}

	 
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	 
	if err := saveKey(path, priv, masterKey); err != nil {
		return nil, err
	}

	return priv, nil
}

func saveKey(path string, priv ed25519.PrivateKey, masterKey []byte) error {
	bytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	pemBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: bytes,
	}

	pemData := pem.EncodeToMemory(pemBlock)

	 
	if len(masterKey) > 0 {
		pemData, err = EncryptBytes(pemData, masterKey)
		if err != nil {
			return err
		}
	}

	return os.WriteFile(path, pemData, 0600)
}

 
type logWriter struct {
	ch chan<- string
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	if w.ch != nil {
		 
		line := strings.TrimSpace(string(p))
		if line != "" {
			w.ch <- fmt.Sprintf("[Tor] %s", line)
		}
	}
	return len(p), nil
}
