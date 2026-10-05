// Command migrate41 把旧表（model.*）迁成 V4.1 对象（ADR-043 §5 / migration.md §4）。
//
// 用法：
//
//	migrate41 --dry-run                 # 只出报告（默认行为，不写任何数据）
//	migrate41 --apply                   # 真执行：先自动备份 gatebox.db，再写对象
//	migrate41 --apply --force           # 覆盖已存在的同 id 对象（默认跳过）
//	migrate41 --apply --json            # 机器可读输出
//	migrate41 --apply --backup-dir DIR  # 指定备份目录
//
// 重要：GateBox 进程必须已停止。BoltDB 是单写者锁，本工具要独占写。
// 本轮（ADR-043 §5）默认只交付报告；--apply 是验收通过后的独立步骤。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/JiangBeta/gatebox/internal/migration"
	"github.com/JiangBeta/gatebox/internal/objects"
	"github.com/JiangBeta/gatebox/internal/repository"
	"github.com/JiangBeta/gatebox/internal/typespec"
)

func main() {
	var (
		apply     = flag.Bool("apply", false, "真正执行迁移（默认只出报告）")
		dryRun    = flag.Bool("dry-run", false, "只出报告（默认即为 true，此参数仅为显式表达）")
		force     = flag.Bool("force", false, "覆盖已存在的同 id 对象（默认跳过，保证幂等）")
		asJSON    = flag.Bool("json", false, "以 JSON 输出报告")
		dataDir   = flag.String("data-dir", os.Getenv("GATEBOX_DATA_DIR"), "数据目录（默认 ./data 或 GATEBOX_DATA_DIR）")
		backupDir = flag.String("backup-dir", "", "备份目录（默认 <data-dir>/backups）")
		verbose   = flag.Bool("v", false, "逐条打印计划")
	)
	flag.Parse()

	if *dataDir == "" {
		*dataDir = "./data"
	}
	if *dryRun && *apply {
		fatal("--dry-run 与 --apply 不能同时给")
	}

	// --json 时 stdout 只出 JSON，人类可读文本一律走 stderr，
	// 否则 migrate41 --apply --json | jq 会被夹杂的文本弄坏。
	text := os.Stdout
	if *asJSON {
		text = os.Stderr
	}

	// 备份必须在打开 DB **之前**做：bbolt 是 mmap + 有 freelist，
	// 直接拷一个打开中的库文件可能拷到尚未落盘的页。
	var backup string
	if *apply {
		stamp := time.Now().Format("20060102-150405")
		dir := *backupDir
		if dir == "" {
			dir = filepath.Join(*dataDir, "backups")
		}
		backup = filepath.Join(dir, "gatebox-"+stamp+".db")
		if err := backupDB(*dataDir, backup); err != nil {
			fatal("备份失败，已中止（不拿真实数据冒险）: %v", err)
		}
		fmt.Fprintf(text, "已备份 → %s\n", backup)
	}

	// 打开前先确认没人在跑：Bolt 锁 1s 超时，报错信息比 "invalid database" 好懂。
	repo, err := repository.Open(*dataDir)
	if err != nil {
		fatal("打开数据库失败（GateBox 是否还在运行？BoltDB 不允许并发写）: %v", err)
	}
	defer repo.Close()

	specs, err := typespec.Load()
	if err != nil {
		fatal("加载类型目录失败: %v", err)
	}
	svc := objects.New(repo, specs).WithSecrets(repo)

	b := migration.NewBuilder(repo, svc, specs)
	plan, err := b.Build()
	if err != nil {
		fatal("生成迁移计划失败: %v", err)
	}
	plan.Sort()

	if !*asJSON {
		printReport(text, plan, *verbose)
	}

	if !*apply {
		if *asJSON {
			emitJSON(plan, nil, "")
		} else {
			fmt.Fprintln(text, "\n（--dry-run：未写任何数据。验收报告无误后加 --apply 执行，执行前会自动备份）")
		}
		return
	}

	res, err := migration.Apply(svc, plan, *force)
	if err != nil {
		fatal("执行迁移失败: %v", err)
	}
	res.Normalize()
	if *asJSON {
		emitJSON(plan, &res, backup)
	} else {
		printApplyResult(text, res)
	}
	if !res.OK() {
		// 不自动回滚：已写入的条目可能正被别的进程读着，静默回滚比留痕迹更危险。
		fmt.Fprintf(os.Stderr,
			"\n有 %d 条写入失败。**没有自动回滚**：已写入的条目仍然生效。\n"+
				"要回到迁移前状态：停掉 GateBox，把备份文件复制回 %s，再启动。\n",
			len(res.Failed), filepath.Join(*dataDir, "db", "gatebox.db"))
		os.Exit(1)
	}
	if !*asJSON {
		fmt.Fprintf(text, "\n迁移完成：写入 %d 条，跳过 %d 条。\n", res.Applied(), len(res.Skipped))
		fmt.Fprintln(text, "下一步：重启 GateBox，打开 V4.1 页面核对；不满意可用上面的备份回滚。")
	}
}

// backupDB 复制 gatebox.db（执行迁移前的自动备份，ADR-043 §5）。
func backupDB(dataDir, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	src := filepath.Join(dataDir, "db", "gatebox.db")
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// emitJSON 输出**一个** JSON 对象（stdout 独占），apply 非空时带上写入结果与备份路径。
func emitJSON(plan migration.Plan, apply *migration.ApplyResult, backup string) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(struct {
		Summary string                 `json:"summary"`
		Stats   migration.Stats        `json:"stats"`
		Plan    migration.Plan         `json:"plan"`
		Backup  string                 `json:"backup,omitempty"`
		Apply   *migration.ApplyResult `json:"apply,omitempty"`
	}{plan.Summarize(), plan.Stats(), plan, backup, apply})
}

func printReport(w io.Writer, plan migration.Plan, verbose bool) {
	fmt.Fprintf(w, "读到的旧数据：")
	keys := make([]string, 0, len(plan.ReadCounts))
	for k := range plan.ReadCounts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, plan.ReadCounts[k]))
	}
	fmt.Fprintf(w, "%s\n\n", strings.Join(parts, " "))

	fmt.Fprintln(w, "── 计划 ──")
	byKind := map[string]int{}
	for _, p := range plan.Planned {
		byKind[p.Kind]++
	}
	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		fmt.Fprintf(w, "  %-12s %d 条\n", k, byKind[k])
	}
	fmt.Fprintln(w)
	if verbose {
		for _, p := range plan.Planned {
			fmt.Fprintf(w, "  [%s] %s/%s ← %s\n", p.Action, p.Kind, p.ID, p.Source)
		}
	}

	if len(plan.Conflicts) > 0 {
		fmt.Fprintf(w, "── 冲突 %d 条（已自动加后缀，需人工确认）──\n", len(plan.Conflicts))
		for _, c := range plan.Conflicts {
			fmt.Fprintf(w, "  %s/%s ← %s\n     原因: %s\n", c.Kind, c.ID, strings.Join(c.Sources, " & "), c.Reason)
		}
		fmt.Fprintln(w)
	}

	if len(plan.Unmappable) > 0 {
		fmt.Fprintf(w, "── 无法映射 %d 条（迁移不做猜测，需手工补）──\n", len(plan.Unmappable))
		for _, u := range plan.Unmappable {
			fmt.Fprintf(w, "  %s\n     原因: %s\n", u.Source, u.Reason)
		}
		fmt.Fprintln(w)
	}

	if len(plan.Warnings) > 0 {
		fmt.Fprintf(w, "── 需确认 %d 条（已迁出，但有地方值得看一眼）──\n", len(plan.Warnings))
		for _, n := range plan.Warnings {
			fmt.Fprintf(w, "  %s\n     说明: %s\n", n.Source, n.Reason)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "── 小结 ──\n  %s\n", plan.Summarize())
}

func printApplyResult(w io.Writer, res migration.ApplyResult) {
	if len(res.Created) > 0 {
		fmt.Fprintf(w, "新建 %d 条\n", len(res.Created))
	}
	if len(res.Overwrote) > 0 {
		fmt.Fprintf(w, "覆盖 %d 条\n", len(res.Overwrote))
	}
	if len(res.Skipped) > 0 {
		fmt.Fprintf(w, "跳过（已存在）%d 条\n", len(res.Skipped))
	}
	for _, f := range res.Failed {
		fmt.Fprintf(os.Stderr, "  ✗ %s: %s\n", f.Target, f.Reason)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "错误: "+format+"\n", args...)
	os.Exit(1)
}
