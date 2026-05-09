package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gvilherme7/zabbix-go/internal/buffer"
	"github.com/gvilherme7/zabbix-go/internal/plugins"
	"github.com/gvilherme7/zabbix-go/internal/zabbix"
)

type Agent struct {
	Client   *zabbix.Client
	Server   string
	Host     string
	Interval int
	Mode     string
	ExitChan <-chan struct{}
}

func (a *Agent) Start() {
	log.Printf("Lightweight Advanced Zabbix Agent service started. Mode: [%s]", a.Mode)

	if a.Mode == "active" {
		a.runActiveScheduler()
		return
	}

	// Trapper mode (global interval push)
	ticker := time.NewTicker(time.Duration(a.Interval) * time.Second)
	defer ticker.Stop()

	a.runCycle()

	for {
		select {
		case <-ticker.C:
			a.runCycle()
		case <-a.ExitChan:
			log.Println("Service stop requested by OS. Doing graceful teardown & buffer flushing...")
			a.flushDiskBuffer()
			return
		}
	}
}

func (a *Agent) runCycle() {
	var keysToCollect []string
	if a.Mode == "active" {
		activeKeys, err := a.fetchActiveChecks()
		if err != nil {
			log.Printf("Failed to fetch active checks config: %v", err)
			return
		}
		keysToCollect = activeKeys
	} else {
		for k := range plugins.Registry {
			keysToCollect = append(keysToCollect, k)
		}
	}
	a.collectKeysAndSend(keysToCollect)
}

type activeItem struct {
	key   string
	delay int
}

func parseDelay(d string) int {
	if d == "" {
		return 60
	}
	if strings.HasSuffix(d, "s") || strings.HasSuffix(d, "m") || strings.HasSuffix(d, "h") {
		if dur, err := time.ParseDuration(d); err == nil {
			return int(dur.Seconds())
		}
	}
	sec, _ := strconv.Atoi(d)
	if sec <= 0 {
		return 60
	}
	return sec
}

func (a *Agent) runActiveScheduler() {
	log.Println("Starting Active Checks Scheduler...")
	var wg sync.WaitGroup
	defer wg.Wait()

	schedule := make(map[string]time.Time)
	items := make(map[string]activeItem)

	refreshTicker := time.NewTicker(120 * time.Second)
	defer refreshTicker.Stop()

	if newItems, err := a.fetchActiveChecksParsed(); err == nil {
		updateActiveSchedule(newItems, &items, &schedule)
	} else {
		log.Printf("Initial active checks fetch failed: %v", err)
	}

	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-refreshTicker.C:
			if newItems, err := a.fetchActiveChecksParsed(); err == nil {
				updateActiveSchedule(newItems, &items, &schedule)
			}
		case now := <-tick.C:
			var due []string
			for key, nextTime := range schedule {
				if now.After(nextTime) || now.Equal(nextTime) {
					due = append(due, key)
					item := items[key]
					schedule[key] = now.Add(time.Duration(item.delay) * time.Second)
				}
			}
			if len(due) > 0 {
				wg.Add(1)
				go func(keys []string) {
					defer wg.Done()
					a.collectKeysAndSend(keys)
				}(due)
			}
		case <-a.ExitChan:
			log.Println("Service stop requested by OS. Doing graceful teardown & buffer flushing...")
			a.flushDiskBuffer()
			return
		}
	}
}

func updateActiveSchedule(newItems []activeItem, items *map[string]activeItem, schedule *map[string]time.Time) {
	currentKeys := make(map[string]bool)
	for _, item := range newItems {
		currentKeys[item.key] = true
		if existing, exists := (*items)[item.key]; !exists || existing.delay != item.delay {
			(*items)[item.key] = item
			(*schedule)[item.key] = time.Now() // Schedule immediately
		}
	}
	for k := range *schedule {
		if !currentKeys[k] {
			delete(*schedule, k)
			delete(*items, k)
		}
	}
}

func (a *Agent) fetchActiveChecksParsed() ([]activeItem, error) {
	req := zabbix.ActiveCheckRequest{
		Request: "active checks",
		Host:    a.Host,
	}
	data, _ := json.Marshal(req)

	resp, err := a.Client.DoReq(data)
	if err != nil {
		return nil, err
	}

	var parsed zabbix.ActiveCheckResponse
	if err := json.Unmarshal(resp, &parsed); err != nil {
		return nil, fmt.Errorf("active checks schema decode failure: %v", err)
	}

	var items []activeItem
	for _, item := range parsed.Data {
		items = append(items, activeItem{
			key:   item.Key,
			delay: parseDelay(item.Delay),
		})
	}
	return items, nil
}

func (a *Agent) fetchActiveChecks() ([]string, error) {
	req := zabbix.ActiveCheckRequest{
		Request: "active checks",
		Host:    a.Host,
	}
	data, _ := json.Marshal(req)

	resp, err := a.Client.DoReq(data)
	if err != nil {
		return nil, err
	}

	var parsed zabbix.ActiveCheckResponse
	if err := json.Unmarshal(resp, &parsed); err != nil {
		return nil, fmt.Errorf("active checks schema decode failure: %v", err)
	}

	var keys []string
	for _, item := range parsed.Data {
		keys = append(keys, item.Key)
	}
	return keys, nil
}

func (a *Agent) collectKeysAndSend(keys []string) {
	var metrics []zabbix.Metric
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, k := range keys {
		plugin, exists := plugins.Registry[k]
		if !exists {
			continue
		}
		wg.Add(1)
		go func(key string, plug plugins.Plugin) {
			defer wg.Done()

			ch := make(chan struct {
				val string
				err error
			}, 1)

			go func() {
				val, err := plug.Collect()
				ch <- struct {
					val string
					err error
				}{val, err}
			}()

			select {
			case res := <-ch:
				if res.err == nil {
					mu.Lock()
					metrics = append(metrics, zabbix.Metric{Host: a.Host, Key: key, Value: res.val})
					mu.Unlock()
				}
			case <-time.After(5 * time.Second):
				log.Printf("Plugin [%s] timed out after 5 seconds", key)
			}
		}(k, plugin)
	}
	wg.Wait()

	if err := a.sendData(metrics); err != nil {
		log.Printf("Send Network Warning: %v (Metrics stored to disk buffer)", err)
	}
}

func (a *Agent) sendData(metrics []zabbix.Metric) error {
	if len(metrics) == 0 {
		return nil
	}
	packet := zabbix.ZabbixPacket{
		Request: "sender data",
		Data:    metrics,
	}
	jsonData, err := json.Marshal(packet)
	if err != nil {
		return err
	}
	_, err = a.Client.DoReq(jsonData)
	if err != nil {
		buffer.WriteMetrics("metrics.dat", metrics)
		return err
	}
	a.flushDiskBuffer()
	return nil
}

func (a *Agent) flushDiskBuffer() {
	metrics := buffer.Flush("metrics.dat")
	if len(metrics) > 0 {
		log.Printf("[Buffer] Flushing %d stacked metrics back to server...", len(metrics))
		a.sendDataRaw(metrics)
	}
}

func (a *Agent) sendDataRaw(metrics []zabbix.Metric) {
	packet := zabbix.ZabbixPacket{
		Request: "sender data",
		Data:    metrics,
	}
	jsonData, _ := json.Marshal(packet)
	if _, err := a.Client.DoReq(jsonData); err != nil {
		buffer.WriteMetrics("metrics.dat", metrics)
	}
}
