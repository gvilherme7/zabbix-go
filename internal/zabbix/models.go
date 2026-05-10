package zabbix

import (
	"crypto/tls"
	"net"
	"strings"
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
	servers    []string
	tlsConfig  *tls.Config
	useTLS     bool
	conn       net.Conn
	mu         sync.Mutex
}

func NewClient(serverAddrs string, useTLS bool, tlsConfig *tls.Config) *Client {
	var servers []string
	for _, s := range strings.Split(serverAddrs, ",") {
		servers = append(servers, strings.TrimSpace(s))
	}
	return &Client{
		servers:   servers,
		useTLS:    useTLS,
		tlsConfig: tlsConfig,
	}
}
