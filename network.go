package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
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

// NetworkManager orchestrates Tor lifecycle, Onion Services, and P2P connections.
type NetworkManager struct {
	TorInstance *tor.Tor
	OnionAddr   string // Local .onion address
	TorDataDir  string // Temporary directory for Tor data

	// Multi-peer management
	Connections      map[string]net.Conn // peer_id -> active connection
	Incoming         chan string         // Channel for multiplexed incoming messages
	Outgoing         chan string         // Global broadcast channel
	ErrorChan        chan error          // Channel for reporting network errors
	SessionKeys      map[string][]byte   // peer_id -> symmetric session key
	PeerNames        map[string]string   // peer_id -> raw name
	PeerColors       map[string]string   // peer_id -> visual color
	PeerDisplayNames map[string]string   // peer_id -> disambiguated name
	MyPeerID         string              // Unique identifier for this instance
	broadcastRunning bool                // Ensures only one broadcast handler runs
	mu               sync.Mutex          // Guards concurrent access to maps and state
}

// NewNetworkManager initializes a new NetworkManager with empty state.
func NewNetworkManager() *NetworkManager {
	return &NetworkManager{
		Incoming:         make(chan string, 10),
		Outgoing:         make(chan string, 10),
		ErrorChan:        make(chan error, 1),
		Connections:      make(map[string]net.Conn),
		SessionKeys:      make(map[string][]byte),
		PeerNames:        make(map[string]string),
		PeerColors:       make(map[string]string),
		PeerDisplayNames: make(map[string]string),
	}
}

// StartTor bootstraps the embedded Tor instance.
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

	// Use a process-unique data directory to avoid resource locks between multiple instances.
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

// Host initializes a Version 3 Onion Service for others to connect to.
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
	nm.MyPeerID = "host"

	if progress != nil {
		progress <- fmt.Sprintf("Listening on %s.onion", nm.OnionAddr)
	}

	go nm.acceptLoop(onion, secret, progress)

	return nil
}

// acceptLoop handles incoming peer connections on the hosted onion service.
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

// handlePeer manages the lifecycle of a newly connected peer, including handshake and disambiguation.
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
	nm.PeerDisplayNames[peerID] = nm.assignDisplayName(peerName)
	displayName := nm.PeerDisplayNames[peerID]
	nm.mu.Unlock()

	if len(nm.Connections) == 1 {
		if progress != nil {
			progress <- "You are not alone."
		}
	} else {
		if progress != nil {
			progress <- fmt.Sprintf("%s joined us.", displayName)
		}
	}

	if len(nm.Connections) > 5 {
		if progress != nil {
			progress <- "Too many peers may impact performance."
		}
	}

	nm.startPeerIO(peerID, conn, sessionKey, displayName, peerColor)
}

// Connect establishes a connection to a remote Onion Service.
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
	nm.MyPeerID = fmt.Sprintf("peer_self_%x", sha256.Sum256([]byte(time.Now().String())))[:16]
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

// CheckConnection performs a lightweight dial to verify if a remote address is reachable.
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

// performHandshakeHost verifies the remote secret sent by a client.
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

// performHandshakeClient sends the hashed secret to the host for verification.
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

// sha256Hash returns the hex-encoded SHA-256 hash of the input string.
func sha256Hash(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

// startPeerIO initializes reading and broadcasting goroutines for an active peer connection.
func (nm *NetworkManager) startPeerIO(peerID string, conn net.Conn, sessionKey []byte, peerName string, peerColor string) {
	// Reader Goroutine
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

			// Parse JSON for metadata updates (name/color).
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

// broadcastHandler multiplexes outgoing messages to all relevant peer connections.
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

// removePeer cleans up internal state and notifies the UI when a peer disconnects.
func (nm *NetworkManager) removePeer(peerID string, peerName string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if conn, exists := nm.Connections[peerID]; exists {
		conn.Close()
		delete(nm.Connections, peerID)
		delete(nm.SessionKeys, peerID)
		delete(nm.PeerNames, peerID)
		delete(nm.PeerColors, peerID)
		delete(nm.PeerDisplayNames, peerID)

		nm.Incoming <- fmt.Sprintf("PEER_LEFT:%s:%s", peerID, peerName)
	}
}

// assignDisplayName generates a unique display name to avoid UI collisions.
func (nm *NetworkManager) assignDisplayName(rawName string) string {
	used := make(map[string]bool)
	for _, dn := range nm.PeerDisplayNames {
		used[dn] = true
	}
	if !used[rawName] {
		return rawName
	}
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s[%d]", rawName, i)
		if !used[candidate] {
			return candidate
		}
	}
}

// GetPeerIDByDisplayName looks up a peer ID by its unique display name.
func (nm *NetworkManager) GetPeerIDByDisplayName(name string) (string, bool) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	for id, dn := range nm.PeerDisplayNames {
		if dn == name {
			return id, true
		}
	}
	return "", false
}

// SendToPeer encrypts and sends a message directly to a specific peer.
func (nm *NetworkManager) SendToPeer(peerID string, msg string) error {
	nm.mu.Lock()
	conn, ok := nm.Connections[peerID]
	key := nm.SessionKeys[peerID]
	nm.mu.Unlock()
	if !ok {
		return fmt.Errorf("peer %s not connected", peerID)
	}
	text := msg
	if len(key) > 0 {
		encrypted, err := EncryptMessage(msg, key)
		if err != nil {
			return fmt.Errorf("encrypt failed: %v", err)
		}
		text = encrypted
	}
	_, err := fmt.Fprintf(conn, "%s\n", text)
	return err
}

// MakePeerChan returns a channel that forwards inputs to SendToPeer for the specified peer ID.
func (nm *NetworkManager) MakePeerChan(peerID string) chan string {
	ch := make(chan string, 20)
	go func() {
		for msg := range ch {
			if err := nm.SendToPeer(peerID, msg); err != nil {
				nm.ErrorChan <- fmt.Errorf("sendto peer %s: %v", peerID, err)
				return
			}
		}
	}()
	return ch
}

// Close gracefully terminates all peer connections and shuts down the Tor instance.
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
	nm.PeerDisplayNames = make(map[string]string)

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

// loadOrGenerateKey loads an Ed25519 private key from disk or creates a new one, supporting optional encryption.
func loadOrGenerateKey(path string, masterKey []byte) (ed25519.PrivateKey, error) {
	if path == "VOID" {
		decoded, _ := base64.StdEncoding.DecodeString(VoidPrivateKeyB64)
		return ed25519.PrivateKey(decoded), nil
	}

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

// saveKey persists an Ed25519 private key to disk in PKCS8 PEM format, optionally encrypted.
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

// logWriter implements io.Writer to pipe internal logs to a status channel.
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

// SHA-256 and secret constants for THE VOID shared instance.
const (
	VoidPrivateKeyB64 = "n2NUTec9DnvJGyrnDO+G5CRYs9/OMHUh9T6wmJ7jow8S/HMOYOj5nvlRQgs4D/sbhvMO6hmSYJBRzYr3T2VnnQ=="
	VoidSecret        = "the_void_secret_1234XYZ"
	VoidAddress       = "cl6hgdta5d4z56kriiftqd73dodpgdxkdgjgbecrzwfpot3fm6owq3id.onion"
)

// JoinOrHostVoid attempts to join the global shared Void instance or hosts it if unreachable.
func (nm *NetworkManager) JoinOrHostVoid(ctx context.Context, progress chan<- string) error {
	if progress != nil {
		progress <- "Entering THE VOID..."
		progress <- fmt.Sprintf("Locating Void at %s", VoidAddress)
	}

	connectCtx, cancelConnect := context.WithTimeout(ctx, 45*time.Second)
	defer cancelConnect()

	err := nm.Connect(connectCtx, VoidAddress, VoidSecret, nil)

	if err == nil {
		if progress != nil {
			progress <- "It whispers back."
		}
		return nil
	}

	if progress != nil {
		progress <- "The room is empty. You are the host."
	}

	err = nm.Host(ctx, "VOID", VoidSecret, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to host THE VOID: %v", err)
	}

	if progress != nil {
		progress <- "Hosting THE VOID. Awaiting others."
	}

	return nil
}
