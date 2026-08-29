package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/shirou/gopsutil/v4/disk"
	gopsutilnet "github.com/shirou/gopsutil/v4/net"
)

func TestCollectFastAvoidsExternalCommands(t *testing.T) {
	origRunCmd := runCmd
	origCommandExists := commandExists
	origPartitions := diskPartitionsFunc
	origUsage := diskUsageFunc
	origIOCounters := ioCountersFunc
	t.Cleanup(func() {
		runCmd = origRunCmd
		commandExists = origCommandExists
		diskPartitionsFunc = origPartitions
		diskUsageFunc = origUsage
		ioCountersFunc = origIOCounters
	})

	var externalCalls atomic.Int32
	runCmd = func(ctx context.Context, name string, args ...string) (string, error) {
		externalCalls.Add(1)
		return "", errors.New("unexpected command")
	}
	commandExists = func(name string) bool {
		externalCalls.Add(1)
		return false
	}
	diskPartitionsFunc = func(all bool) ([]disk.PartitionStat, error) {
		return []disk.PartitionStat{
			{Device: "/dev/disk3s1s1", Mountpoint: "/", Fstype: "apfs"},
		}, nil
	}
	diskUsageFunc = func(path string) (*disk.UsageStat, error) {
		return &disk.UsageStat{
			Path:        path,
			Fstype:      "apfs",
			Total:       2 * 1024 * 1024 * 1024,
			Used:        1024 * 1024 * 1024,
			UsedPercent: 50,
		}, nil
	}
	ioCountersFunc = func(bool) ([]gopsutilnet.IOCountersStat, error) {
		return []gopsutilnet.IOCountersStat{
			{Name: "en0", BytesRecv: 1024, BytesSent: 2048},
		}, nil
	}

	collector := NewCollector(ProcessWatchOptions{})
	if _, err := collector.CollectFast(); err != nil {
		t.Fatalf("CollectFast() error = %v", err)
	}
	if externalCalls.Load() != 0 {
		t.Fatalf("CollectFast() made %d external command calls", externalCalls.Load())
	}
}

func TestCollectProcessesKeepsLiveProcessesWithCachedEnrichment(t *testing.T) {
	origPartitions := diskPartitionsFunc
	origUsage := diskUsageFunc
	origIOCounters := ioCountersFunc
	origCollectProcesses := collectProcessesFunc
	t.Cleanup(func() {
		diskPartitionsFunc = origPartitions
		diskUsageFunc = origUsage
		ioCountersFunc = origIOCounters
		collectProcessesFunc = origCollectProcesses
	})

	diskPartitionsFunc = func(all bool) ([]disk.PartitionStat, error) {
		return []disk.PartitionStat{
			{Device: "/dev/disk3s1s1", Mountpoint: "/", Fstype: "apfs"},
		}, nil
	}
	diskUsageFunc = func(path string) (*disk.UsageStat, error) {
		return &disk.UsageStat{
			Path:        path,
			Fstype:      "apfs",
			Total:       2 * 1024 * 1024 * 1024,
			Used:        1024 * 1024 * 1024,
			UsedPercent: 50,
		}, nil
	}
	ioCountersFunc = func(bool) ([]gopsutilnet.IOCountersStat, error) {
		return []gopsutilnet.IOCountersStat{{Name: "en0", BytesRecv: 1024, BytesSent: 2048}}, nil
	}
	collectProcessesFunc = func() (processSample, error) {
		return processSample{processes: []ProcessInfo{
			{PID: 200, PPID: 1, Name: "new-hot-process", Command: "/usr/bin/new-hot-process", CPU: 240, Memory: 1.5},
			{PID: 201, PPID: 200, State: "Z+", Name: "defunct-child", Command: "defunct-child"},
		}, parentsAvailable: true}, nil
	}

	collector := NewCollector(ProcessWatchOptions{Enabled: true, CPUThreshold: 50})
	cachedZombieCount := 7
	cachedParentsComplete := true
	cached := MetricsSnapshot{
		Hardware:  HardwareInfo{Model: "MacBook Pro"},
		TrashSize: 99,
		TopProcesses: []ProcessInfo{
			{PID: 100, Name: "old-process", CPU: 10},
		},
		ZombieCount: &cachedZombieCount,
		ZombieParents: []ZombieParent{
			{PID: 100, Name: "old-process", Count: 7},
		},
		ZombieParentsComplete: &cachedParentsComplete,
		ProcessAlerts: []ProcessAlert{
			{PID: 100, Name: "old-process", Status: "active"},
		},
	}
	collector.cacheEnrichment(cached)
	collector.cacheProcessEnrichment(cached)

	snapshot, err := collector.CollectProcesses()
	if err != nil {
		t.Fatalf("CollectProcesses() error = %v", err)
	}

	if snapshot.Hardware.Model != "MacBook Pro" || snapshot.TrashSize != 99 {
		t.Fatalf("expected cached enrichment to be preserved, got hardware=%#v trash=%d", snapshot.Hardware, snapshot.TrashSize)
	}
	if len(snapshot.TopProcesses) < 1 || snapshot.TopProcesses[0].Name != "new-hot-process" {
		t.Fatalf("expected live top process data, got %#v", snapshot.TopProcesses)
	}
	if len(snapshot.ProcessAlerts) != 1 || snapshot.ProcessAlerts[0].Name != "new-hot-process" {
		t.Fatalf("expected live process alert data, got %#v", snapshot.ProcessAlerts)
	}
	if snapshot.ZombieCount == nil || *snapshot.ZombieCount != 1 || len(snapshot.ZombieParents) != 1 || snapshot.ZombieParents[0].PID != 200 {
		t.Fatalf("expected live zombie summary, got count=%v parents=%#v", snapshot.ZombieCount, snapshot.ZombieParents)
	}
	if snapshot.ZombieParentsComplete == nil || !*snapshot.ZombieParentsComplete {
		t.Fatalf("expected complete live parent attribution, got %v", snapshot.ZombieParentsComplete)
	}

	fastSnapshot, err := collector.CollectFast()
	if err != nil {
		t.Fatalf("CollectFast() error = %v", err)
	}
	if fastSnapshot.ZombieCount == nil || *fastSnapshot.ZombieCount != 1 ||
		len(fastSnapshot.ZombieParents) != 1 || fastSnapshot.ZombieParents[0].PID != 200 {
		t.Fatalf("fast refresh restored stale zombies: count=%v parents=%#v", fastSnapshot.ZombieCount, fastSnapshot.ZombieParents)
	}
}

func TestSlowEnrichmentDoesNotReplaceLatestProcessSummary(t *testing.T) {
	collector := NewCollector(ProcessWatchOptions{})
	measured := 3
	complete := true
	collector.cacheProcessEnrichment(MetricsSnapshot{
		ZombieCount: &measured,
		ZombieParents: []ZombieParent{
			{PID: 42, Name: "Chrome", Count: 3},
		},
		ZombieParentsComplete: &complete,
	})

	collector.cacheEnrichment(MetricsSnapshot{Hardware: HardwareInfo{Model: "newer enrichment"}})
	var snapshot MetricsSnapshot
	collector.applyEnrichment(&snapshot, false)

	if snapshot.ZombieCount == nil || *snapshot.ZombieCount != 3 {
		t.Fatalf("latest measured zombie count was not preserved: %v", snapshot.ZombieCount)
	}
	if len(snapshot.ZombieParents) != 1 || snapshot.ZombieParents[0].PID != 42 {
		t.Fatalf("latest measured zombie parents were not preserved: %#v", snapshot.ZombieParents)
	}
	if snapshot.ZombieParentsComplete == nil || !*snapshot.ZombieParentsComplete {
		t.Fatalf("latest parent completeness was not preserved: %v", snapshot.ZombieParentsComplete)
	}
}
