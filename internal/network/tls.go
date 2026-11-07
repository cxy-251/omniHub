package network

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// TLSStatus 描述内置 CA 与 HTTPS 服务端证书状态。
type TLSStatus struct {
	Enabled       bool     `json:"enabled"`
	CAExists      bool     `json:"ca_exists"`
	CAFingerprint string   `json:"ca_fingerprint,omitempty"`
	CertExpiresAt string   `json:"cert_expires_at,omitempty"`
	SANs          []string `json:"sans"`
	ExtraHosts    []string `json:"extra_hosts"`
	HTTPSURLs     []string `json:"https_urls"`
}

// TLSManager 管理本地 Root CA 及自动签发的多 SAN 服务端证书。
type TLSManager struct {
	dir        string
	mu         sync.RWMutex
	enabled    bool
	extraHosts []string
	caCert     *x509.Certificate
	caKey      *ecdsa.PrivateKey
	caPEM      []byte
	leafCert   *tls.Certificate
	leafX509   *x509.Certificate
	sans       []string
}

// NewTLSManager 初始化 TLS 证书管理器。
func NewTLSManager(configDir string, enabled bool, extraHosts []string) (*TLSManager, error) {
	m := &TLSManager{
		dir:        filepath.Join(configDir, "tls"),
		enabled:    enabled,
		extraHosts: normalizeHosts(extraHosts),
	}
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return nil, err
	}
	_ = m.loadOrInit(false)
	return m, nil
}

func normalizeHosts(in []string) []string {
	var out []string
	for _, h := range in {
		for _, part := range strings.FieldsFunc(h, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
		}) {
			clean := strings.ToLower(strings.TrimSpace(part))
			clean = strings.TrimPrefix(clean, "https://")
			clean = strings.TrimPrefix(clean, "http://")
			if host, _, err := net.SplitHostPort(clean); err == nil && host != "" {
				clean = host
			}
			if clean != "" && !slices.Contains(out, clean) {
				out = append(out, clean)
			}
		}
	}
	return out
}

func (m *TLSManager) caCertPath() string   { return filepath.Join(m.dir, "root-ca.pem") }
func (m *TLSManager) caKeyPath() string    { return filepath.Join(m.dir, "root-ca-key.pem") }
func (m *TLSManager) leafCertPath() string { return filepath.Join(m.dir, "server-cert.pem") }
func (m *TLSManager) leafKeyPath() string  { return filepath.Join(m.dir, "server-key.pem") }

func (m *TLSManager) loadOrInit(forceRegenLeaf bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.ensureCA(); err != nil {
		return err
	}
	return m.ensureLeaf(forceRegenLeaf)
}

func (m *TLSManager) ensureCA() error {
	certBytes, err1 := os.ReadFile(m.caCertPath())
	keyBytes, err2 := os.ReadFile(m.caKeyPath())
	if err1 == nil && err2 == nil {
		certBlock, _ := pem.Decode(certBytes)
		keyBlock, _ := pem.Decode(keyBytes)
		if certBlock != nil && keyBlock != nil {
			caCert, err := x509.ParseCertificate(certBlock.Bytes)
			if err == nil {
				caKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
				if err == nil && time.Now().Before(caCert.NotAfter.Add(-30*24*time.Hour)) {
					m.caCert = caCert
					m.caKey = caKey
					m.caPEM = certBytes
					return nil
				}
			}
		}
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := randSerial()
	if err != nil {
		return err
	}
	now := time.Now().Add(-10 * time.Minute)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"omniHub Local Authority"},
			CommonName:   "omniHub Local Root CA",
		},
		NotBefore:             now,
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return err
	}
	caCert, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.WriteFile(m.caCertPath(), certPEM, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(m.caKeyPath(), keyPEM, 0o600); err != nil {
		return err
	}
	m.caCert = caCert
	m.caKey = priv
	m.caPEM = certPEM
	return nil
}

func (m *TLSManager) desiredSANs() ([]string, []net.IP, []string) {
	dnsNames := []string{"localhost", "omnihub.local"}
	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	all := []string{"localhost", "omnihub.local", "127.0.0.1", "::1"}

	addHost := func(h string) {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" || slices.Contains(all, h) {
			return
		}
		if ip := net.ParseIP(h); ip != nil {
			ips = append(ips, ip)
			all = append(all, h)
		} else {
			dnsNames = append(dnsNames, h)
			all = append(all, h)
		}
	}

	for _, ip := range GetLocalIPs() {
		addHost(ip)
	}
	for _, h := range m.extraHosts {
		addHost(h)
	}
	return dnsNames, ips, all
}

func (m *TLSManager) ensureLeaf(force bool) error {
	dnsNames, ips, allSANs := m.desiredSANs()

	if !force {
		certPEM, err1 := os.ReadFile(m.leafCertPath())
		keyPEM, err2 := os.ReadFile(m.leafKeyPath())
		if err1 == nil && err2 == nil {
			pair, err := tls.X509KeyPair(certPEM, keyPEM)
			if err == nil && len(pair.Certificate) > 0 {
				leaf, err := x509.ParseCertificate(pair.Certificate[0])
				if err == nil && time.Now().Before(leaf.NotAfter.Add(-7*24*time.Hour)) && leafCoversAll(leaf, allSANs) {
					m.leafCert = &pair
					m.leafX509 = leaf
					m.sans = allSANs
					return nil
				}
			}
		}
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := randSerial()
	if err != nil {
		return err
	}
	now := time.Now().Add(-10 * time.Minute)
	// Apple iOS/macOS 要求 TLS 终端服务器证书有效期不超过 825 天（此处取 800 天）。
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"omniHub Server"},
			CommonName:   "omnihub.local",
		},
		NotBefore:   now,
		NotAfter:    now.Add(800 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    dnsNames,
		IPAddresses: ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, m.caCert, &priv.PublicKey, m.caKey)
	if err != nil {
		return err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.WriteFile(m.leafCertPath(), certPEM, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(m.leafKeyPath(), keyPEM, 0o600); err != nil {
		return err
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return err
	}
	m.leafCert = &pair
	m.leafX509 = leaf
	m.sans = allSANs
	return nil
}

func leafCoversAll(leaf *x509.Certificate, targets []string) bool {
	for _, t := range targets {
		if ip := net.ParseIP(t); ip != nil {
			found := false
			for _, lip := range leaf.IPAddresses {
				if lip.Equal(ip) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		} else {
			if !slices.Contains(leaf.DNSNames, t) {
				return false
			}
		}
	}
	return true
}

func randSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

// Configure 更新 HTTPS 开关与附加域名/IP，并按需重签发服务器证书。
func (m *TLSManager) Configure(enabled bool, extraHosts []string, forceRegen bool) error {
	m.mu.Lock()
	m.enabled = enabled
	if extraHosts != nil {
		m.extraHosts = normalizeHosts(extraHosts)
	}
	m.mu.Unlock()
	return m.loadOrInit(forceRegen)
}

// Enabled 返回是否开启了内置 HTTPS。
func (m *TLSManager) Enabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

// TLSConfig 返回供同端口多路复用监听器使用的 *tls.Config。
func (m *TLSManager) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			if hello != nil && hello.ServerName != "" {
				sni := strings.ToLower(strings.TrimSpace(hello.ServerName))
				m.mu.RLock()
				covered := m.leafX509 != nil && leafCoversAll(m.leafX509, []string{sni})
				m.mu.RUnlock()
				if !covered {
					m.mu.Lock()
					if !slices.Contains(m.extraHosts, sni) {
						m.extraHosts = append(m.extraHosts, sni)
					}
					_ = m.ensureLeaf(true)
					m.mu.Unlock()
				}
			}
			m.mu.RLock()
			defer m.mu.RUnlock()
			return m.leafCert, nil
		},
	}
}

// Status 返回当前证书与 HTTPS 链接状态。
func (m *TLSManager) Status(port string) TLSStatus {
	_ = m.loadOrInit(false)
	m.mu.RLock()
	defer m.mu.RUnlock()

	var fp string
	if m.caCert != nil {
		sum := sha256.Sum256(m.caCert.Raw)
		fp = strings.ToUpper(hex.EncodeToString(sum[:12]))
	}
	var exp string
	if m.leafX509 != nil {
		exp = m.leafX509.NotAfter.Format("2006-01-02")
	}
	var urls []string
	for _, ip := range GetLocalIPs() {
		if parsed := net.ParseIP(ip); parsed != nil && parsed.IsPrivate() {
			urls = append(urls, "https://"+ip+":"+port)
		}
	}
	for _, h := range m.extraHosts {
		u := "https://" + h + ":" + port
		if !slices.Contains(urls, u) {
			urls = append(urls, u)
		}
	}
	return TLSStatus{
		Enabled:       m.enabled,
		CAExists:      len(m.caPEM) > 0,
		CAFingerprint: fp,
		CertExpiresAt: exp,
		SANs:          slices.Clone(m.sans),
		ExtraHosts:    slices.Clone(m.extraHosts),
		HTTPSURLs:     urls,
	}
}

// RootCAPEM 返回 PEM 格式根证书内容。
func (m *TLSManager) RootCAPEM() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.caPEM)
}

// RootCAMobileConfig 生成 iOS / macOS 一键安装描述文件（.mobileconfig）。
func (m *TLSManager) RootCAMobileConfig() []byte {
	m.mu.RLock()
	caCert := m.caCert
	m.mu.RUnlock()
	if caCert == nil {
		return nil
	}
	b64 := base64.StdEncoding.EncodeToString(caCert.Raw)
	xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadCertificateFileName</key>
			<string>omnihub-root-ca.cer</string>
			<key>PayloadContent</key>
			<data>%s</data>
			<key>PayloadDescription</key>
			<string>安装 omniHub 本地根证书以消除浏览器 HTTPS 不安全提示</string>
			<key>PayloadDisplayName</key>
			<string>omniHub Local Root CA</string>
			<key>PayloadIdentifier</key>
			<string>local.omnihub.ca.cert</string>
			<key>PayloadType</key>
			<string>com.apple.security.root</string>
			<key>PayloadUUID</key>
			<string>6A91F230-8B1E-4C4B-9D71-001122334455</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
		</dict>
	</array>
	<key>PayloadDescription</key>
	<string>信任 omniHub 自建根证书，使局域网与广域网 IP/域名直连支持安全 HTTPS</string>
	<key>PayloadDisplayName</key>
	<string>omniHub HTTPS 根证书</string>
	<key>PayloadIdentifier</key>
	<string>local.omnihub.ca.profile</string>
	<key>PayloadOrganization</key>
	<string>omniHub</string>
	<key>PayloadRemovalDisallowed</key>
	<false/>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>6A91F230-8B1E-4C4B-9D71-AABBCCDDEEFF</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>
`, b64)
	return []byte(xml)
}

// dualProtocolListener 在同一个 TCP 端口上自动识别 TLS 握手（首字节 0x16）与明文 HTTP。
type dualProtocolListener struct {
	net.Listener
	tlsMgr *TLSManager
}

type peekedConn struct {
	net.Conn
	r io.Reader
}

func (c *peekedConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

func (l *dualProtocolListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if l.tlsMgr == nil || !l.tlsMgr.Enabled() {
		return conn, nil
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1)
	n, err := conn.Read(buf)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil || n == 0 {
		_ = conn.Close()
		return nil, err
	}
	pc := &peekedConn{
		Conn: conn,
		r:    io.MultiReader(bytes.NewReader(buf[:n]), conn),
	}
	// 0x16 是 TLS Handshake 记录头标识。
	if buf[0] == 0x16 {
		return tls.Server(pc, l.tlsMgr.TLSConfig()), nil
	}
	return pc, nil
}
