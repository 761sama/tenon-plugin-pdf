package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.761sama.com/tenon-plugin-pdf/jsongen"
)

// fontFlag 收集重复的 -font 标志：id=路径[@ttc序号] 或 id=builtin:内置名。
type fontFlag [][2]string

func (f *fontFlag) String() string { return fmt.Sprint([][2]string(*f)) }

func (f *fontFlag) Set(v string) error {
	id, src, ok := strings.Cut(v, "=")
	if !ok || id == "" || src == "" {
		return fmt.Errorf("字体参数应为 id=路径[@ttc序号] 或 id=builtin:内置名，得到 %q", v)
	}
	*f = append(*f, [2]string{id, src})
	return nil
}

// cmdJSON 从 JSON 描述生成 PDF。字体一律经 -font 标志在代码层注册，
// JSON 内不允许出现字体路径（安全约束，见 doc/json-format.md）。
func cmdJSON(args []string) error {
	fs := flag.NewFlagSet("json", flag.ExitOnError)
	out := fs.String("o", "json-out.pdf", "输出文件")
	var fonts fontFlag
	fs.Var(&fonts, "font", `注册字体：id=字体.ttf|字体.ttc@序号 或 id=builtin:Helvetica-Bold（可重复）`)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("用法: tenon-pdf json [-o out.pdf] [-font id=路径[@序号]]... <doc.json>")
	}

	reg := jsongen.NewFontRegistry()
	for _, f := range fonts {
		id, src := f[0], f[1]
		var err error
		if name, ok := strings.CutPrefix(src, "builtin:"); ok {
			err = reg.RegisterBuiltin(id, name)
		} else if path, idx, ok := strings.Cut(src, "@"); ok {
			n, cerr := strconv.Atoi(idx)
			if cerr != nil {
				return fmt.Errorf("字体 %s: TTC 序号 %q 非法", id, idx)
			}
			err = reg.RegisterCollection(id, path, n)
		} else {
			err = reg.RegisterFile(id, src)
		}
		if err != nil {
			return err
		}
	}

	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	doc, err := jsongen.Build(data, reg)
	if err != nil {
		return err
	}
	return doc.SaveFile(*out)
}
