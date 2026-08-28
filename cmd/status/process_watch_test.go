package main

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Guards #1267: ps and uptime emit comma decimals under locales like
// ru_RU.UTF-8, which made every ParseFloat in the collectors fail.
func TestRunCmdForcesCLocale(t *testing.T) {
	t.Setenv("LC_ALL", "ru_RU.UTF-8")
	t.Setenv("LC_NUMERIC", "ru_RU.UTF-8")
	t.Setenv("LANG", "ru_RU.UTF-8")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := runCmd(ctx, "sh", "-c", "printf '%s|%s|%s' \"$LC_ALL\" \"$LC_NUMERIC\" \"$LANG\"")
	if err != nil {
		t.Fatalf("runCmd() error = %v", err)
	}
	if out != "C||" {
		t.Fatalf("runCmd() subprocess locale = %q, want %q", out, "C||")
	}
}

func TestCollectProcessesUnderCommaLocale(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("ps output format is darwin-specific")
	}
	t.Setenv("LC_ALL", "ru_RU.UTF-8")
	t.Setenv("LC_NUMERIC", "ru_RU.UTF-8")

	procs, err := collectProcesses()
	if err != nil {
		t.Fatalf("collectProcesses() error = %v", err)
	}
	if len(procs) == 0 {
		t.Fatal("collectProcesses() returned no processes under a comma-decimal locale")
	}
}

func TestParsePsAuxOutputStrictAcceptsCurrentDarwinOutput(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("ps output format is darwin-specific")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := runCmd(ctx, "ps", "aux")
	if err != nil {
		t.Fatalf("ps aux error = %v", err)
	}
	procs, err := parsePsAuxOutputStrict(out)
	if err != nil {
		t.Fatalf("parsePsAuxOutputStrict(current output) error = %v", err)
	}
	if len(procs) == 0 {
		t.Fatal("parsePsAuxOutputStrict(current output) returned no processes")
	}
}

func TestParsePrimaryProcessOutputStrictAcceptsCurrentDarwinOutput(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("ps output format is darwin-specific")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := runCmd(ctx, "ps", "-Aceo", "pid=,ppid=,state=,pcpu=,pmem=,rss=,comm=", "-r")
	if err != nil {
		t.Fatalf("primary ps command error = %v", err)
	}
	procs, err := parseProcessOutputStrict(out)
	if err != nil {
		t.Fatalf("parseProcessOutputStrict(current output) error = %v", err)
	}
	if len(procs) == 0 {
		t.Fatal("parseProcessOutputStrict(current output) returned no processes")
	}
}

func TestParseProcessOutput(t *testing.T) {
	raw := strings.Join([]string{
		"123 1 145.2 10.1 7340032 /Applications/Visual Studio Code.app/Contents/MacOS/Electron",
		"456 1 99.5 2.2 262144 /System/Library/CoreServices/Finder.app/Contents/MacOS/Finder",
		"bad line",
	}, "\n")

	procs := parseProcessOutput(raw)
	if len(procs) != 2 {
		t.Fatalf("parseProcessOutput() len = %d, want 2", len(procs))
	}

	if procs[0].PID != 123 || procs[0].PPID != 1 {
		t.Fatalf("unexpected pid/ppid: %+v", procs[0])
	}
	if procs[0].Name != "Electron" {
		t.Fatalf("unexpected process name %q", procs[0].Name)
	}
	if !strings.Contains(procs[0].Command, "Visual Studio Code.app") {
		t.Fatalf("command path missing spaces: %q", procs[0].Command)
	}
	if procs[0].MemoryBytes != 7340032*1024 {
		t.Fatalf("unexpected memory bytes %d", procs[0].MemoryBytes)
	}
}

func TestParseProcessOutputKeepsOldFiveColumnShape(t *testing.T) {
	raw := "123 1 145.2 10.1 /Applications/Visual Studio Code.app/Contents/MacOS/Electron"

	procs := parseProcessOutput(raw)
	if len(procs) != 1 {
		t.Fatalf("parseProcessOutput() len = %d, want 1", len(procs))
	}
	if procs[0].Memory != 10.1 {
		t.Fatalf("unexpected memory percent %.1f", procs[0].Memory)
	}
	if procs[0].MemoryBytes != 0 {
		t.Fatalf("old ps shape should not invent memory bytes, got %d", procs[0].MemoryBytes)
	}
	if procs[0].Command != "/Applications/Visual Studio Code.app/Contents/MacOS/Electron" {
		t.Fatalf("unexpected command %q", procs[0].Command)
	}
}

func TestParseProcessOutputCapturesZombieState(t *testing.T) {
	raw := strings.Join([]string{
		"123 10 Z+ 0.0 0.0 0 worker <defunct>",
		"456 10 S 1.5 0.2 1024 /usr/bin/worker",
	}, "\n")

	procs := parseProcessOutput(raw)
	if len(procs) != 2 {
		t.Fatalf("parseProcessOutput() len = %d, want 2", len(procs))
	}
	if procs[0].State != "Z+" {
		t.Fatalf("zombie state = %q, want Z+", procs[0].State)
	}
	if procs[1].State != "S" {
		t.Fatalf("ordinary state = %q, want S", procs[1].State)
	}
}

func TestParsePsAuxOutputCapturesResidentMemory(t *testing.T) {
	raw := strings.Join([]string{
		"USER PID %CPU %MEM VSZ RSS TT STAT STARTED TIME COMMAND",
		"raj 123 4.5 6.0 123456 2097152 ?? S 10:00AM 1:23 /Applications/Chrome.app/Contents/MacOS/Chrome --type=renderer",
	}, "\n")

	procs := parsePsAuxOutput(raw)
	if len(procs) != 1 {
		t.Fatalf("parsePsAuxOutput() len = %d, want 1", len(procs))
	}
	if procs[0].MemoryBytes != 2097152*1024 {
		t.Fatalf("unexpected memory bytes %d", procs[0].MemoryBytes)
	}
	if procs[0].State != "S" {
		t.Fatalf("unexpected process state %q", procs[0].State)
	}
	if !strings.Contains(procs[0].Command, "--type=renderer") {
		t.Fatalf("command path missing args: %q", procs[0].Command)
	}
}

func TestFallbackCountsZombiesWithoutGuessingParents(t *testing.T) {
	raw := strings.Join([]string{
		"USER PID %CPU %MEM VSZ RSS TT STAT STARTED TIME COMMAND",
		"raj 10 1.0 1.0 100 10 ?? S 10:00AM 0:01 /Applications/Chrome.app/Contents/MacOS/Chrome",
		"raj 11 0.0 0.0 0 0 ?? Z+ 10:00AM 0:00 child <defunct>",
	}, "\n")
	procs, err := parsePsAuxOutputStrict(raw)
	if err != nil {
		t.Fatalf("parsePsAuxOutputStrict() error = %v", err)
	}

	count, parents := summarizeZombies(procs, 3)
	if count != 1 {
		t.Fatalf("fallback zombie count = %d, want 1", count)
	}
	if len(parents) != 0 {
		t.Fatalf("fallback should omit unknown parent detail, got %#v", parents)
	}
}

func TestStrictProcessParsersRejectPartialTables(t *testing.T) {
	if _, err := parseProcessOutputStrict("123 1 Z 0.0 0.0 0 child\nbad line"); err == nil {
		t.Fatal("parseProcessOutputStrict() accepted a partial process table")
	}
	for name, raw := range map[string]string{
		"missing state": "123 1 0.0 0.0 0 child",
		"bad ppid":      "123 parent Z 0.0 0.0 0 child",
		"bad rss":       "123 1 Z 0.0 0.0 unknown child",
		"reordered":     "123 1 0.0 Z 0.0 0 child",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseProcessOutputStrict(raw); err == nil {
				t.Fatalf("parseProcessOutputStrict(%q) accepted malformed output", raw)
			}
		})
	}
	if _, err := parsePsAuxOutputStrict("USER PID %CPU %MEM VSZ RSS TT STAT STARTED TIME COMMAND\nbad line"); err == nil {
		t.Fatal("parsePsAuxOutputStrict() accepted a partial process table")
	}
	malformedState := "USER PID %CPU %MEM VSZ RSS TT STAT STARTED TIME COMMAND\nraj 11 0.0 0.0 0 0 ?? ZOMBIE 10:00AM 0:00 child"
	if _, err := parsePsAuxOutputStrict(malformedState); err == nil {
		t.Fatal("parsePsAuxOutputStrict() accepted malformed STAT")
	}
	reordered := "USER PID %MEM %CPU VSZ RSS TT STAT STARTED TIME COMMAND\nraj 11 0.0 0.0 0 0 ?? Z 10:00AM 0:00 child"
	if _, err := parsePsAuxOutputStrict(reordered); err == nil {
		t.Fatal("parsePsAuxOutputStrict() accepted reordered columns")
	}
}

func TestProcessStateTokenGrammar(t *testing.T) {
	for _, state := range []string{"D", "I", "R", "S", "T", "U", "W", "Z", "Z+", "Ss", "R<", "SN+", "S>", "RE", "SV", "RX", "SAW"} {
		if !isProcessStateToken(state) {
			t.Fatalf("isProcessStateToken(%q) = false, want true", state)
		}
	}
	for _, state := range []string{"", "ZOMBIE", "sleeping", "0", "Z?"} {
		if isProcessStateToken(state) {
			t.Fatalf("isProcessStateToken(%q) = true, want false", state)
		}
	}
}

func TestSummarizeZombiesCountsAndRanksKnownParents(t *testing.T) {
	processes := []ProcessInfo{
		{PID: 10, Name: "Chrome"},
		{PID: 20, Name: "zsh"},
		{PID: 101, PPID: 10, State: "Z", Name: "child-a"},
		{PID: 102, PPID: 10, State: "Z+", Name: "child-b"},
		{PID: 103, PPID: 20, State: "Z", Name: "child-c"},
		{PID: 104, PPID: 999, State: "Z", Name: "unknown-parent"},
		{PID: 105, PPID: 10, State: "S", Name: "live-child"},
	}

	count, parents := summarizeZombies(processes, 3)
	if count != 4 {
		t.Fatalf("zombie count = %d, want 4", count)
	}
	if len(parents) != 2 {
		t.Fatalf("zombie parents = %#v, want 2 known parents", parents)
	}
	if parents[0] != (ZombieParent{PID: 10, Name: "Chrome", Count: 2}) {
		t.Fatalf("first zombie parent = %#v", parents[0])
	}
	if parents[1] != (ZombieParent{PID: 20, Name: "zsh", Count: 1}) {
		t.Fatalf("second zombie parent = %#v", parents[1])
	}

	_, limited := summarizeZombies(processes, 1)
	if len(limited) != 1 || limited[0].PID != 10 {
		t.Fatalf("limited zombie parents = %#v", limited)
	}
}

func TestTopProcessesSortsByCPU(t *testing.T) {
	procs := []ProcessInfo{
		{PID: 3, Name: "low", CPU: 20, Memory: 3},
		{PID: 1, Name: "high", CPU: 120, Memory: 1},
		{PID: 2, Name: "mid", CPU: 120, Memory: 8},
	}

	top := topProcesses(procs, 2)
	if len(top) != 2 {
		t.Fatalf("topProcesses() len = %d, want 2", len(top))
	}
	if top[0].PID != 2 || top[1].PID != 1 {
		t.Fatalf("unexpected order: %+v", top)
	}
}

func TestProcessNameFromCommand(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{"/Applications/Visual Studio Code.app/Contents/MacOS/Electron", "Electron"},
		{"/usr/local/bin/node /tmp/server.js", "server.js"},
		{"Finder", "Finder"},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			if got := processNameFromCommand(tt.command); got != tt.want {
				t.Fatalf("processNameFromCommand(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}

func TestProcessWatcherTriggersAfterContinuousWindow(t *testing.T) {
	base := time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)
	watcher := NewProcessWatcher(ProcessWatchOptions{
		Enabled:      true,
		CPUThreshold: 100,
		Window:       5 * time.Minute,
	})

	proc := []ProcessInfo{{PID: 42, Name: "stress", CPU: 140}}
	if alerts := watcher.Update(base, proc); len(alerts) != 0 {
		t.Fatalf("unexpected early alerts: %+v", alerts)
	}
	if alerts := watcher.Update(base.Add(4*time.Minute), proc); len(alerts) != 0 {
		t.Fatalf("unexpected early alerts at 4m: %+v", alerts)
	}
	alerts := watcher.Update(base.Add(5*time.Minute), proc)
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert after full window, got %+v", alerts)
	}
	if alerts[0].Status != "active" {
		t.Fatalf("unexpected alert status %q", alerts[0].Status)
	}
}

func TestProcessWatcherResetsWhenUsageDrops(t *testing.T) {
	base := time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)
	watcher := NewProcessWatcher(ProcessWatchOptions{
		Enabled:      true,
		CPUThreshold: 100,
		Window:       5 * time.Minute,
	})

	high := []ProcessInfo{{PID: 42, Name: "stress", CPU: 140}}
	low := []ProcessInfo{{PID: 42, Name: "stress", CPU: 30}}

	watcher.Update(base, high)
	watcher.Update(base.Add(4*time.Minute), high)
	if alerts := watcher.Update(base.Add(4*time.Minute+30*time.Second), low); len(alerts) != 0 {
		t.Fatalf("expected reset after dip, got %+v", alerts)
	}
	if alerts := watcher.Update(base.Add(9*time.Minute), high); len(alerts) != 0 {
		t.Fatalf("expected no alert after reset, got %+v", alerts)
	}
	if alerts := watcher.Update(base.Add(14*time.Minute), high); len(alerts) != 1 {
		t.Fatalf("expected alert after second full window, got %+v", alerts)
	}
}

func TestProcessWatcherResetsOnPIDReuse(t *testing.T) {
	base := time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)
	watcher := NewProcessWatcher(ProcessWatchOptions{
		Enabled:      true,
		CPUThreshold: 100,
		Window:       2 * time.Minute,
	})

	firstProc := []ProcessInfo{{
		PID:     42,
		PPID:    1,
		Name:    "stress",
		Command: "/usr/bin/stress",
		CPU:     140,
	}}
	secondProc := []ProcessInfo{{
		PID:     42,
		PPID:    99,
		Name:    "node",
		Command: "/usr/local/bin/node /tmp/server.js",
		CPU:     135,
	}}

	watcher.Update(base, firstProc)
	if alerts := watcher.Update(base.Add(2*time.Minute), firstProc); len(alerts) != 1 {
		t.Fatalf("expected first process to alert after window, got %+v", alerts)
	}

	if alerts := watcher.Update(base.Add(3*time.Minute), secondProc); len(alerts) != 0 {
		t.Fatalf("expected pid reuse to reset tracking, got %+v", alerts)
	}
	if alerts := watcher.Update(base.Add(5*time.Minute), secondProc); len(alerts) != 1 {
		t.Fatalf("expected reused pid to alert only after its own window, got %+v", alerts)
	}
}

func TestRenderProcessAlertBar(t *testing.T) {
	alerts := []ProcessAlert{
		{PID: 10, Name: "node", CPU: 150, Threshold: 100, Window: "5m0s", Status: "active"},
		{PID: 11, Name: "java", CPU: 130, Threshold: 100, Window: "5m0s", Status: "active"},
	}

	bar := renderProcessAlertBar(alerts, 120)
	if !strings.Contains(bar, "ALERT") {
		t.Fatalf("missing alert prefix: %q", bar)
	}
	if !strings.Contains(bar, "node (10)") {
		t.Fatalf("missing lead process label: %q", bar)
	}
	if !strings.Contains(bar, "+1 more") {
		t.Fatalf("missing additional alert count: %q", bar)
	}
	if strings.Contains(bar, "terminate") || strings.Contains(bar, "ignore") {
		t.Fatalf("unexpected action text in read-only alert bar: %q", bar)
	}
}

func TestMetricsSnapshotJSONIncludesProcessWatch(t *testing.T) {
	zombieCount := 2
	snapshot := MetricsSnapshot{
		ZombieCount: &zombieCount,
		ZombieParents: []ZombieParent{{
			PID:   42,
			Name:  "Chrome",
			Count: 2,
		}},
		ProcessWatch: ProcessWatchConfig{
			Enabled:      true,
			CPUThreshold: 100,
			Window:       "5m0s",
		},
		ProcessAlerts: []ProcessAlert{{
			PID:       99,
			Name:      "node",
			CPU:       140,
			Threshold: 100,
			Window:    "5m0s",
			Status:    "active",
		}},
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "\"process_watch\"") {
		t.Fatalf("missing process_watch in json: %s", out)
	}
	if !strings.Contains(out, "\"process_alerts\"") {
		t.Fatalf("missing process_alerts in json: %s", out)
	}
	if !strings.Contains(out, "\"zombie_count\":2") || !strings.Contains(out, "\"zombie_parents\"") {
		t.Fatalf("missing zombie summary in json: %s", out)
	}
}

func TestMetricsSnapshotJSONDistinguishesUnmeasuredFromZeroZombies(t *testing.T) {
	unmeasured, err := json.Marshal(MetricsSnapshot{})
	if err != nil {
		t.Fatalf("json.Marshal(unmeasured) error = %v", err)
	}
	if strings.Contains(string(unmeasured), "\"zombie_count\"") {
		t.Fatalf("unmeasured snapshot should omit zombie_count: %s", unmeasured)
	}

	zero := 0
	measured, err := json.Marshal(MetricsSnapshot{ZombieCount: &zero})
	if err != nil {
		t.Fatalf("json.Marshal(measured) error = %v", err)
	}
	if !strings.Contains(string(measured), "\"zombie_count\":0") {
		t.Fatalf("measured zero snapshot should include zombie_count: %s", measured)
	}
}
