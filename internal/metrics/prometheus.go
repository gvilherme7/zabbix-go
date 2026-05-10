package metrics

import (
	"fmt"
	"net/http"
	"runtime"
)

// StartPrometheusServer launches a simple HTTP listener for /metrics
func StartPrometheusServer(port int) {
	if port == 0 {
		return
	}
	
	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		fmt.Fprintf(w, "# HELP zabbix_agent_alloc_bytes Memory allocated and still in use.\n")
		fmt.Fprintf(w, "# TYPE zabbix_agent_alloc_bytes gauge\n")
		fmt.Fprintf(w, "zabbix_agent_alloc_bytes %d\n", m.Alloc)

		fmt.Fprintf(w, "# HELP zabbix_agent_sys_bytes Total memory obtained from the OS.\n")
		fmt.Fprintf(w, "# TYPE zabbix_agent_sys_bytes gauge\n")
		fmt.Fprintf(w, "zabbix_agent_sys_bytes %d\n", m.Sys)

		fmt.Fprintf(w, "# HELP zabbix_agent_goroutines Number of goroutines that currently exist.\n")
		fmt.Fprintf(w, "# TYPE zabbix_agent_goroutines gauge\n")
		fmt.Fprintf(w, "zabbix_agent_goroutines %d\n", runtime.NumGoroutine())
	})

	go func() {
		http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	}()
}
