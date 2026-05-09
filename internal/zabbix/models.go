package zabbix

import (
	"crypto/tls"
	"net"
	"sync"
)

type Metric struct {
	Host  string `json:"host"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type ZabbixPacket struct {
	Request string   `json:"request"`
	Data    []Metric `json:"data"`
}

type ActiveCheckRequest struct {
	Request string `json:"request"`
	Host    string `json:"host"`
}

type ActiveCheckResponse struct {
	Response string `json:"response"`
	Data     []struct {
		Key   string `json:"key"`
		Delay string `json:"delay"`
	} `json:"data"`
}

type Client struct {
	serverAddr string
	tlsConfig  *tls.Config
	useTLS     bool
	conn       net.Conn
	mu         sync.Mutex
}

func NewClient(serverAddr string, useTLS bool, tlsConfig *tls.Config) *Client {
	return &Client{
		serverAddr: serverAddr,
		useTLS:     useTLS,
		tlsConfig:  tlsConfig,
	}
}
