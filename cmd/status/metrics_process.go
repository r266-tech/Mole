package main

import (
	"container/heap"
	"context"
	"fmt"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

var collectProcessesFunc = collectProcesses

const zombieParentLimit = 3

func collectProcesses() ([]ProcessInfo, error) {
	if runtime.GOOS != "darwin" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := runCmd(ctx, "ps", "-Aceo", "pid=,ppid=,state=,pcpu=,pmem=,rss=,comm=", "-r")
	if err == nil {
		if procs, parseErr := parseProcessOutputStrict(out); parseErr == nil {
			return procs, nil
		}
	}

	out, err = runCmd(ctx, "ps", "aux")
	if err != nil {
		return nil, err
	}
	return parsePsAuxOutputStrict(out)
}

func parseProcessOutputStrict(raw string) ([]ProcessInfo, error) {
	rows := strings.Split(strings.TrimSpace(raw), "\n")
	if len(rows) == 0 || (len(rows) == 1 && rows[0] == "") {
		return nil, fmt.Errorf("empty ps process table")
	}

	procs := make([]ProcessInfo, 0, len(rows))
	for _, row := range rows {
		fields := strings.Fields(row)
		if len(fields) < 7 || !isProcessStateToken(fields[2]) {
			return nil, fmt.Errorf("unexpected ps process row")
		}
		pid, pidErr := strconv.Atoi(fields[0])
		ppid, ppidErr := strconv.Atoi(fields[1])
		cpuVal, cpuErr := strconv.ParseFloat(fields[3], 64)
		memVal, memErr := strconv.ParseFloat(fields[4], 64)
		rssKB, rssErr := strconv.ParseUint(fields[5], 10, 64)
		command := strings.Join(fields[6:], " ")
		if pidErr != nil || ppidErr != nil || cpuErr != nil || memErr != nil || rssErr != nil ||
			pid <= 0 || ppid < 0 || command == "" {
			return nil, fmt.Errorf("unexpected ps process row")
		}
		procs = append(procs, ProcessInfo{
			PID:         pid,
			PPID:        ppid,
			State:       fields[2],
			Name:        processNameFromCommand(command),
			Command:     command,
			CPU:         cpuVal,
			Memory:      memVal,
			MemoryBytes: rssKB * 1024,
		})
	}
	return procs, nil
}

func parseProcessOutput(raw string) []ProcessInfo {
	procs := make([]ProcessInfo, 0, strings.Count(raw, "\n"))
	for line := range strings.Lines(strings.TrimSpace(raw)) {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			continue
		}
		ppid, _ := strconv.Atoi(fields[1])
		metricStart := 2
		state := ""
		cpuVal, err := strconv.ParseFloat(fields[metricStart], 64)
		if err != nil {
			if len(fields) < 6 || !isProcessStateToken(fields[2]) {
				continue
			}
			state = fields[2]
			metricStart++
			cpuVal, err = strconv.ParseFloat(fields[metricStart], 64)
			if err != nil {
				continue
			}
		}
		memVal, err := strconv.ParseFloat(fields[metricStart+1], 64)
		if err != nil {
			continue
		}

		rssBytes := uint64(0)
		commandStart := metricStart + 2
		if len(fields) >= metricStart+4 {
			if rssKB, err := strconv.ParseUint(fields[metricStart+2], 10, 64); err == nil {
				rssBytes = rssKB * 1024
				commandStart = metricStart + 3
			}
		}

		command := strings.Join(fields[commandStart:], " ")
		if command == "" {
			continue
		}
		procs = append(procs, ProcessInfo{
			PID:         pid,
			PPID:        ppid,
			State:       state,
			Name:        processNameFromCommand(command),
			Command:     command,
			CPU:         cpuVal,
			Memory:      memVal,
			MemoryBytes: rssBytes,
		})
	}
	return procs
}

// parsePsAuxOutput parses the fallback "ps aux" format.
// Columns: USER PID %CPU %MEM VSZ RSS TT STAT STARTED TIME COMMAND
func parsePsAuxOutput(raw string) []ProcessInfo {
	procs := make([]ProcessInfo, 0, strings.Count(raw, "\n"))
	first := true
	for line := range strings.Lines(strings.TrimSpace(raw)) {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil || pid <= 0 {
			continue
		}
		cpuVal, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			continue
		}
		memVal, err := strconv.ParseFloat(fields[3], 64)
		if err != nil {
			continue
		}
		rssKB, err := strconv.ParseUint(fields[5], 10, 64)
		if err != nil {
			rssKB = 0
		}
		command := strings.Join(fields[10:], " ")
		if command == "" {
			continue
		}
		procs = append(procs, ProcessInfo{
			PID:         pid,
			PPID:        0,
			State:       fields[7],
			Name:        processNameFromCommand(command),
			Command:     command,
			CPU:         cpuVal,
			Memory:      memVal,
			MemoryBytes: rssKB * 1024,
		})
	}
	return procs
}

func parsePsAuxOutputStrict(raw string) ([]ProcessInfo, error) {
	rows := strings.Split(strings.TrimSpace(raw), "\n")
	expectedHeader := []string{"USER", "PID", "%CPU", "%MEM", "VSZ", "RSS", "TT", "STAT", "STARTED", "TIME", "COMMAND"}
	if len(rows) < 2 || !slices.Equal(strings.Fields(rows[0]), expectedHeader) {
		return nil, fmt.Errorf("unexpected ps aux header")
	}

	procs := make([]ProcessInfo, 0, len(rows)-1)
	for _, row := range rows[1:] {
		fields := strings.Fields(row)
		if len(fields) < 11 || !isProcessStateToken(fields[7]) {
			return nil, fmt.Errorf("unexpected ps aux process row")
		}
		pid, pidErr := strconv.Atoi(fields[1])
		cpuVal, cpuErr := strconv.ParseFloat(fields[2], 64)
		memVal, memErr := strconv.ParseFloat(fields[3], 64)
		_, vszErr := strconv.ParseUint(fields[4], 10, 64)
		rssKB, rssErr := strconv.ParseUint(fields[5], 10, 64)
		command := strings.Join(fields[10:], " ")
		if pidErr != nil || cpuErr != nil || memErr != nil || vszErr != nil || rssErr != nil || pid <= 0 || command == "" {
			return nil, fmt.Errorf("unexpected ps aux process row")
		}
		procs = append(procs, ProcessInfo{
			PID:         pid,
			PPID:        0,
			State:       fields[7],
			Name:        processNameFromCommand(command),
			Command:     command,
			CPU:         cpuVal,
			Memory:      memVal,
			MemoryBytes: rssKB * 1024,
		})
	}
	return procs, nil
}

func isProcessStateToken(state string) bool {
	if state == "" {
		return false
	}
	if !strings.ContainsRune("DIRSTUWZ", rune(state[0])) {
		return false
	}
	for _, modifier := range state[1:] {
		if !strings.ContainsRune("+<>AELNSsVWX", modifier) {
			return false
		}
	}
	return true
}

func isZombieState(state string) bool {
	return strings.HasPrefix(strings.TrimSpace(state), "Z")
}

func summarizeZombies(processes []ProcessInfo, limit int) (int, []ZombieParent) {
	byPID := make(map[int]ProcessInfo, len(processes))
	for _, proc := range processes {
		byPID[proc.PID] = proc
	}

	count := 0
	byParent := make(map[int]int)
	for _, proc := range processes {
		if !isZombieState(proc.State) {
			continue
		}
		count++
		if proc.PPID <= 0 {
			continue
		}
		parent, ok := byPID[proc.PPID]
		if !ok || parent.Name == "" {
			continue
		}
		byParent[proc.PPID]++
	}

	parents := make([]ZombieParent, 0, len(byParent))
	for ppid, zombieCount := range byParent {
		parents = append(parents, ZombieParent{
			PID:   ppid,
			Name:  byPID[ppid].Name,
			Count: zombieCount,
		})
	}
	sort.Slice(parents, func(i, j int) bool {
		if parents[i].Count != parents[j].Count {
			return parents[i].Count > parents[j].Count
		}
		if parents[i].Name != parents[j].Name {
			return parents[i].Name < parents[j].Name
		}
		return parents[i].PID < parents[j].PID
	})
	if limit <= 0 {
		return count, nil
	}
	if len(parents) > limit {
		parents = parents[:limit]
	}
	return count, parents
}

func processNameFromCommand(command string) string {
	name := command
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if spIdx := strings.Index(name, " "); spIdx >= 0 {
		name = name[:spIdx]
	}
	return name
}

func topProcesses(processes []ProcessInfo, limit int) []ProcessInfo {
	if limit <= 0 || len(processes) == 0 {
		return nil
	}

	h := &processHeap{}
	heap.Init(h)
	for _, proc := range processes {
		if h.Len() < limit {
			heap.Push(h, proc)
			continue
		}
		if processRanksBefore(proc, (*h)[0]) {
			heap.Pop(h)
			heap.Push(h, proc)
		}
	}

	top := make([]ProcessInfo, h.Len())
	for i := range slices.Backward(top) {
		top[i] = heap.Pop(h).(ProcessInfo)
	}
	return top
}

func formatProcessLabel(proc ProcessInfo) string {
	if proc.Name != "" {
		return fmt.Sprintf("%s (%d)", proc.Name, proc.PID)
	}
	return fmt.Sprintf("pid %d", proc.PID)
}

func processRanksBefore(a, b ProcessInfo) bool {
	if a.CPU != b.CPU {
		return a.CPU > b.CPU
	}
	if a.Memory != b.Memory {
		return a.Memory > b.Memory
	}
	return a.PID < b.PID
}

type processHeap []ProcessInfo

func (h processHeap) Len() int { return len(h) }

func (h processHeap) Less(i, j int) bool {
	return processRanksBefore(h[j], h[i])
}

func (h processHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *processHeap) Push(x any) {
	*h = append(*h, x.(ProcessInfo))
}

func (h *processHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}
