// Command eval 运行安全副驾的产品评估。见 docs/architecture.md 第 5.13 节。
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "仓库根目录，evals/ 的父目录")
	set := fs.String("set", "all", "运行的集合：tuning、holdout 或 all")
	provider := fs.String("provider", "", "评估用的供应商：anthropic 或 deepseek")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "未知的参数 %q\n", fs.Arg(0))
		fs.Usage()
		return 2
	}
	switch *set {
	case "tuning", "holdout", "all":
	default:
		fmt.Fprintf(stderr, "-set 只能是 tuning、holdout 或 all，得到 %q\n", *set)
		return 2
	}
	switch *provider {
	case "", "anthropic", "deepseek":
	default:
		fmt.Fprintf(stderr, "-provider 只能是 anthropic 或 deepseek，得到 %q\n", *provider)
		return 2
	}
	return runEval(evalConfig{Root: *root, Set: *set, Provider: *provider}, stdout, stderr)
}
