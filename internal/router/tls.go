package router

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Authority struct {
	Certificate *x509.Certificate
	Key         *ecdsa.PrivateKey
	PEM         []byte
	dir         string
	mu          sync.Mutex
	cache       map[string]*tls.Certificate
}

func serial() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		panic(err)
	}
	return n
}
func LoadAuthority(dir string) (*Authority, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	certPath, keyPath := filepath.Join(dir, "root.crt"), filepath.Join(dir, "root.key")
	certPEM, ce := os.ReadFile(certPath)
	keyPEM, ke := os.ReadFile(keyPath)
	if errors.Is(ce, os.ErrNotExist) && errors.Is(ke, os.ErrNotExist) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		template := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: "Loca Local Development CA", Organization: []string{"Loca"}}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, MaxPathLen: 0, MaxPathLenZero: true}
		der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if err != nil {
			return nil, err
		}
		rawKey, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return nil, err
		}
		certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: rawKey})
		if err = AtomicWrite(keyPath, keyPEM, 0600); err != nil {
			return nil, err
		}
		if err = AtomicWrite(certPath, certPEM, 0644); err != nil {
			return nil, err
		}
	} else if ce != nil || ke != nil {
		return nil, fmt.Errorf("cannot load Loca certificate/key (do not delete either independently): %v / %v", ce, ke)
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	key, ok := pair.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("unexpected CA key type")
	}
	if time.Now().After(cert.NotAfter) {
		return nil, fmt.Errorf("Loca root certificate expired; renew and trust a new local CA")
	}
	return &Authority{Certificate: cert, Key: key, PEM: certPEM, dir: dir, cache: map[string]*tls.Certificate{}}, nil
}
func (a *Authority) Fingerprint() string {
	sum := sha256.Sum256(a.Certificate.Raw)
	return hex.EncodeToString(sum[:])
}
func (a *Authority) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	name, err := Name(hello.ServerName)
	if err != nil || hello.ServerName != name+".localhost" {
		return nil, fmt.Errorf("unrecognized TLS server name")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if cert := a.cache[name]; cert != nil && time.Until(cert.Leaf.NotAfter) > 24*time.Hour {
		return cert, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: hello.ServerName}, DNSNames: []string{hello.ServerName}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(0, 1, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, a.Certificate, &key.PublicKey, a.Key)
	if err != nil {
		return nil, err
	}
	cert := &tls.Certificate{Certificate: [][]byte{der, a.Certificate.Raw}, PrivateKey: key, Leaf: template}
	a.cache[name] = cert
	return cert, nil
}
