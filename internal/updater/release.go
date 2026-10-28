package updater

import "time"

type Asset struct {
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Signature []byte `json:"-"`
	Layout    string `json:"layout"`
	Entry     string `json:"entry"`
}

type Release struct {
	Version     string    `json:"version"`
	Build       string    `json:"build"`
	Date        string    `json:"date"`
	Notes       string    `json:"notes"`
	Mandatory   bool      `json:"mandatory"`
	Installable bool      `json:"installable"`
	Reason      string    `json:"reason,omitempty"`
	Asset       Asset     `json:"asset"`
	PublishedAt time.Time `json:"-"`
}
