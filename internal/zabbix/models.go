package zabbix

import (
	"crypto/tls"
	"net"
	"strings"
	"sync"
)

type Metric struct {
	Id    int    `json:"id,omitempty"`
	Host  string `json:"host"`
	Key   string `json:"key"`
	Value string `json:"value"`
	State int    `json:"state,omitempty"`
	Clock int64  `json:"clock,omitempty"`
	Ns    int    `json:"ns,omitempty"`
}

type ZabbixPacket struct {
	Request string   `json:"request"`
	Session string   `json:"session,omitempty"`
	Clock   int64    `json:"clock,omitempty"`
	Ns      int      `json:"ns,omitempty"`
	Data    []Metric `json:"data"`
}

type ActiveCheckRequest struct {
	Request string `json:"request"`
	Host    string `json:"host"`
}

type ActiveCheckResponse struct {
	Response string `json:"response"`
	Data     []struct {
		Key   string      `json:"key"`
		Delay interface{} `json:"delay"`
	} `json:"data"`
}

type Client struct {
	servers    []string
	tlsConfig  *tls.Config
	useTLS     bool
	compress   bool
	conn       net.Conn
	mu         sync.Mutex
}

func NewClient(serverAddrs string, useTLS bool, tlsConfig *tls.Config, compress bool) *Client {
	var servers []string
	for _, s := range strings.Split(serverAddrs, ",") {
		servers = append(servers, strings.TrimSpace(s))
	}
	return &Client{
		servers:   servers,
		useTLS:    useTLS,
		tlsConfig: tlsConfig,
		compress:  compress,
	}
}
