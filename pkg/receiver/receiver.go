package receiver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"

	"legacytel/pkg/config"
	"legacytel/pkg/model"
)

// Receiver defines a unified interface for legacy platform data connectors.
type Receiver interface {
	// Start begins listening to/polling/simulating legacy logs and routes parsed events into the output channel.
	Start(ctx context.Context, outputChan chan<- *model.LogRecord) error
	// Stop gracefully shuts down the receiver listeners.
	Stop() error
	// GetName returns the human-readable identifier of the receiver.
	GetName() string
}

// ListenSecureTCP establishes a network listener on the specified port.
// If TLS is enabled in the configuration, it loads certificates and returns a secure TLS listener.
// If client_ca_file is provided, it configures Mutual TLS (mTLS) for cryptographic client verification.
func ListenSecureTCP(settings config.ReceiverSettings) (net.Listener, error) {
	addr := fmt.Sprintf("%s:%d", settings.BindAddress, settings.Port)

	if !settings.TLSEnabled {
		// Return insecure plain TCP listener (use with caution in production)
		return net.Listen("tcp", addr)
	}

	// 1. Load server certificate and private key
	cert, err := tls.LoadX509KeyPair(settings.CertFile, settings.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS key pair (cert: %s, key: %s): %w", settings.CertFile, settings.KeyFile, err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12, // Force secure modern TLS protocols
	}

	// 2. Configure Mutual TLS (mTLS) if client CA is provided
	if settings.ClientCAFile != "" {
		caCert, err := ioutil.ReadFile(settings.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read client CA file (%s): %w", settings.ClientCAFile, err)
		}

		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse client CA certificate")
		}

		tlsConfig.ClientCAs = caPool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert // mTLS enforcement
	}

	return tls.Listen("tcp", addr, tlsConfig)
}
