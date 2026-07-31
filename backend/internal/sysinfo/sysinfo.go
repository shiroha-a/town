// Package sysinfo reads the machine's and this process's resource usage from
// /proc and the filesystem, for the operator's dashboard.
//
// コンテナの中から読むので、ホスト側の負荷とメモリは機械全体の値になる。
// 「余裕があるか」を見る用途なのでそれで足りるが、それだけでは自分のプロセスが
// 太っているのかが分からないので、プロセス自身のぶんも分けて返す。
//
// 取れない値は0のままにして先へ進む。ここで失敗してダッシュボード全体が
// 出なくなるほうが困る。
package sysinfo

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// clockTicks is Linux's USER_HZ. /proc/self/stat のCPU時間はこの刻みで入る。
// sysconf(_SC_CLK_TCK) を引くにはcgoが要るが、Linuxでは100で固定されている。
const clockTicks = 100.0

// Host is the machine as a whole.
type Host struct {
	// Load1/5/15 は待ち行列の長さ。CPU数と並べて見ないと意味が無いので一緒に返す。
	Load1       float64 `json:"load1"`
	Load5       float64 `json:"load5"`
	Load15      float64 `json:"load15"`
	CPUs        int     `json:"cpus"`
	MemTotalKB  int64   `json:"mem_total_kb"`
	MemFreeKB   int64   `json:"mem_free_kb"` // MemAvailable(実際に使える量)
	SwapTotalKB int64   `json:"swap_total_kb"`
	SwapFreeKB  int64   `json:"swap_free_kb"`
	DiskTotalB  int64   `json:"disk_total_b"`
	DiskFreeB   int64   `json:"disk_free_b"`
	// Proc はこのプロセス自身の使用量。
	Proc Proc `json:"proc"`
}

// Proc is what this process itself is using. 増え続けていないか(漏れていないか)
// を見るためのもの。
type Proc struct {
	RSSBytes   int64   `json:"rss_bytes"` // 実メモリ(常駐)。実際に食っている量
	VMSBytes   int64   `json:"vms_bytes"` // 仮想メモリ。予約ぶんを含む
	CPUSeconds float64 `json:"cpu_seconds"`
	// CPUPercent は直近の使用率。1コアを使い切って100%。
	CPUPercent float64 `json:"cpu_percent"`
	Threads    int     `json:"threads"`
	OpenFDs    int     `json:"open_fds"` // 開いているファイル/ソケットの数
	Goroutines int     `json:"goroutines"`
	HeapAllocB int64   `json:"heap_alloc_b"`
	SysB       int64   `json:"sys_b"` // Goが確保している総量
	UptimeSec  int64   `json:"uptime_sec"`
	GoVersion  string  `json:"go_version"`
}

// started is when this process came up. 稼働時間はここからの差で出す。
var started = time.Now()

// Read gathers everything. cpuSample は使用率を出すために2回測る間隔で、
// 0以下ならCPU使用率は測らない(累計だけ返す)。
func Read(diskPath string, cpuSample time.Duration) Host {
	h := Host{CPUs: runtime.NumCPU()}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(b))
		if len(f) >= 3 {
			h.Load1, _ = strconv.ParseFloat(f[0], 64)
			h.Load5, _ = strconv.ParseFloat(f[1], 64)
			h.Load15, _ = strconv.ParseFloat(f[2], 64)
		}
	}
	readMeminfo(&h)

	var st syscall.Statfs_t
	if err := syscall.Statfs(diskPath, &st); err == nil {
		h.DiskTotalB = int64(st.Blocks) * int64(st.Bsize)
		// Bavail は非特権ユーザーが使える量。Bfree は予約ぶんを含むので、
		// 「あとどれだけ書けるか」に近いこちらを出す。
		h.DiskFreeB = int64(st.Bavail) * int64(st.Bsize)
	}
	h.Proc = readProc(cpuSample)
	return h
}

// readProc measures this process. CPU使用率は累計の差分でしか出せないので、
// cpuSample のあいだ空けて2回測る。
func readProc(cpuSample time.Duration) Proc {
	p := Proc{Goroutines: runtime.NumGoroutine(), GoVersion: runtime.Version()}
	p.UptimeSec = int64(time.Since(started).Seconds())

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	p.HeapAllocB = int64(ms.HeapAlloc)
	p.SysB = int64(ms.Sys)

	readSelfStatus(&p)
	if fds, err := os.ReadDir("/proc/self/fd"); err == nil {
		// 自分がこのディレクトリを読むために開いたぶんを1つ引く。
		p.OpenFDs = len(fds) - 1
	}

	cpu0, ok := cpuSeconds()
	if !ok {
		return p
	}
	p.CPUSeconds = cpu0
	if cpuSample <= 0 {
		return p
	}
	t0 := time.Now()
	time.Sleep(cpuSample)
	cpu1, ok := cpuSeconds()
	if !ok {
		return p
	}
	if elapsed := time.Since(t0).Seconds(); elapsed > 0 {
		p.CPUSeconds = cpu1
		p.CPUPercent = (cpu1 - cpu0) / elapsed * 100
	}
	return p
}

// cpuSeconds returns the CPU time this process has used so far.
func cpuSeconds() (float64, bool) {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, false
	}
	// 2つめの項目(実行ファイル名)は括弧に入っていて空白も括弧も含みうる。
	// 最後の ')' から後ろを見るのが確実。
	i := strings.LastIndexByte(string(b), ')')
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(string(b)[i+1:])
	// この並びの先頭は3項目め(state)。utime=14, stime=15 なので添字は11と12。
	if len(f) < 13 {
		return 0, false
	}
	utime, err1 := strconv.ParseFloat(f[11], 64)
	stime, err2 := strconv.ParseFloat(f[12], 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return (utime + stime) / clockTicks, true
}

// readSelfStatus pulls memory and thread counts out of /proc/self/status.
func readSelfStatus(p *Proc) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(val), " kB"), 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "VmRSS":
			p.RSSBytes = n * 1024
		case "VmSize":
			p.VMSBytes = n * 1024
		case "Threads":
			p.Threads = int(n)
		}
	}
}

// readMeminfo pulls the four numbers we care about out of /proc/meminfo.
func readMeminfo(h *Host) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(val), " kB"), 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			h.MemTotalKB = n
		case "MemAvailable":
			h.MemFreeKB = n
		case "SwapTotal":
			h.SwapTotalKB = n
		case "SwapFree":
			h.SwapFreeKB = n
		}
	}
}
