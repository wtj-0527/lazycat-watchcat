package protocol

import "time"

const Version = "v1"

type CreatePairingCodeResponse struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type PairCollectorRequest struct {
	Code         string   `json:"code"`
	Name         string   `json:"name"`
	Hostname     string   `json:"hostname"`
	OSVersion    string   `json:"osVersion,omitempty"`
	CollectorVer string   `json:"collectorVersion"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type PairCollectorResponse struct {
	DeviceID             string    `json:"deviceId"`
	Token                string    `json:"token"`
	CertificatePEM       string    `json:"certificatePem"`
	PrivateKeyPEM        string    `json:"privateKeyPem"`
	CACertificatePEM     string    `json:"caCertificatePem"`
	CertificateSerial    string    `json:"certificateSerial"`
	CertificateExpiresAt time.Time `json:"certificateExpiresAt"`
}

type Device struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Hostname     string            `json:"hostname"`
	OSVersion    string            `json:"osVersion,omitempty"`
	CollectorVer string            `json:"collectorVersion"`
	Capabilities []string          `json:"capabilities"`
	Group        string            `json:"group,omitempty"`
	Location     string            `json:"location,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Status       string            `json:"status"`
	CreatedAt    time.Time         `json:"createdAt"`
	LastSeenAt   time.Time         `json:"lastSeenAt"`
}

type MetricPoint struct {
	Name        string            `json:"name"`
	Value       float64           `json:"value"`
	Unit        string            `json:"unit,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	CollectedAt time.Time         `json:"collectedAt"`
}

type MetricBatch struct {
	DeviceID string        `json:"deviceId"`
	Points   []MetricPoint `json:"points"`
}
