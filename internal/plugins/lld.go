package plugins

import (
	"encoding/json"
	"net"
)

func init() {
	Registry["net.if.discovery"] = &FuncPlugin{
		key: "net.if.discovery",
		fn: func() (string, error) {
			interfaces, err := net.Interfaces()
			if err != nil {
				return "", err
			}
			
			var lldData []map[string]string
			for _, iface := range interfaces {
				lldData = append(lldData, map[string]string{
					"{#IFNAME}": iface.Name,
				})
			}
			
			jsonData, err := json.Marshal(lldData)
			return string(jsonData), err
		},
	}
	
	Registry["vfs.fs.discovery"] = &FuncPlugin{
		key: "vfs.fs.discovery",
		fn: func() (string, error) {
			// Simplified cross-platform fallback
			lldData := []map[string]string{
				{"{#FSNAME}": "/", "{#FSTYPE}": "rootfs"},
			}
			jsonData, err := json.Marshal(lldData)
			return string(jsonData), err
		},
	}
}
