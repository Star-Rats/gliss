package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"main.go":                    "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n",
		"internal/util/util.go":      "package util\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n",
		"internal/util/util_test.go": "package util\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fail()\n\t}\n}\n",
		"docs/readme.md":             "# Gliss\n\nA coding agent.\n",
		"bin/data.bin":               "\x00\x01\x02binary",
	}
	for path, content := range files {
		abs := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("结果应包含 %q:\n%q", want, got)
	}
}

func TestRegistryHasAllTools(t *testing.T) {
	r := NewRegistry(setupRoot(t), 0)
	want := []string{"read_file", "write_file", "edit_file", "list_dir", "glob", "grep", "run_bash"}
	for _, name := range want {
		if _, ok := r.Get(name); !ok {
			t.Errorf("缺少工具 %s", name)
		}
	}
	if len(r.Definitions()) != len(want) {
		t.Errorf("Definitions 数量 = %d, 期望 %d", len(r.Definitions()), len(want))
	}
}

func TestReadFile(t *testing.T) {
	r := NewRegistry(setupRoot(t), 0)
	out := r.Execute(context.Background(), "read_file", `{"path":"main.go"}`)
	mustContain(t, out, "1\tpackage main")
	mustContain(t, out, "3\tfunc main()")

	out = r.Execute(context.Background(), "read_file", `{"path":"main.go","offset":3,"limit":1}`)
	mustContain(t, out, "3\tfunc main()")
	if strings.Contains(out, "package main") {
		t.Errorf("offset 生效失败: %q", out)
	}

	out = r.Execute(context.Background(), "read_file", `{"path":"不存在.go"}`)
	mustContain(t, out, "错误")
}

func TestPathEscape(t *testing.T) {
	r := NewRegistry(setupRoot(t), 0)
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"../secret.txt",
		filepath.Join(t.TempDir(), "elsewhere.txt"),
		"main.go/../../../secret.txt",
	} {
		out := r.Execute(context.Background(), "read_file", `{"path":"`+p+`"}`)
		mustContain(t, out, "超出工作目录范围")
	}
}

func TestWriteFileAndEdit(t *testing.T) {
	root := setupRoot(t)
	r := NewRegistry(root, 0)

	out := r.Execute(context.Background(), "write_file", `{"path":"new/dir/file.txt","content":"line1\nline2\nline2\n"}`)
	mustContain(t, out, "已写入 new/dir/file.txt")
	data, err := os.ReadFile(filepath.Join(root, "new/dir/file.txt"))
	if err != nil || string(data) != "line1\nline2\nline2\n" {
		t.Fatalf("写入内容不符: %q, err=%v", data, err)
	}

	// 不唯一且未 replace_all → 报错
	out = r.Execute(context.Background(), "edit_file", `{"path":"new/dir/file.txt","old_string":"line2","new_string":"X"}`)
	mustContain(t, out, "2 处")

	// replace_all
	out = r.Execute(context.Background(), "edit_file", `{"path":"new/dir/file.txt","old_string":"line2","new_string":"X","replace_all":true}`)
	mustContain(t, out, "替换 2 处")

	// 唯一匹配
	out = r.Execute(context.Background(), "edit_file", `{"path":"new/dir/file.txt","old_string":"line1\nX","new_string":"Y"}`)
	mustContain(t, out, "替换 1 处")

	// 未找到
	out = r.Execute(context.Background(), "edit_file", `{"path":"new/dir/file.txt","old_string":"nope","new_string":"Y"}`)
	mustContain(t, out, "未找到")

	// 相同串
	out = r.Execute(context.Background(), "edit_file", `{"path":"new/dir/file.txt","old_string":"a","new_string":"a"}`)
	mustContain(t, out, "相同")
}

func TestListDir(t *testing.T) {
	r := NewRegistry(setupRoot(t), 0)
	out := r.Execute(context.Background(), "list_dir", `{}`)
	mustContain(t, out, "main.go (")
	mustContain(t, out, "internal/")
	mustContain(t, out, "docs/")
	mustContain(t, out, "bin/")
}

func TestGlob(t *testing.T) {
	r := NewRegistry(setupRoot(t), 0)
	out := r.Execute(context.Background(), "glob", `{"pattern":"**/*.go"}`)
	mustContain(t, out, "main.go")
	mustContain(t, out, "internal/util/util.go")
	mustContain(t, out, "internal/util/util_test.go")
	if strings.Contains(out, "readme.md") {
		t.Errorf("glob 不应匹配 md: %q", out)
	}

	out = r.Execute(context.Background(), "glob", `{"pattern":"*.go"}`)
	mustContain(t, out, "main.go")
	if strings.Contains(out, "internal/") {
		t.Errorf("*.go 不应递归: %q", out)
	}
}

func TestGrep(t *testing.T) {
	r := NewRegistry(setupRoot(t), 0)
	out := r.Execute(context.Background(), "grep", `{"pattern":"func \\w+"}`)
	mustContain(t, out, "main.go:3:func main() {")
	mustContain(t, out, "internal/util/util.go:3:func Add(a, b int) int {")
	mustContain(t, out, "internal/util/util_test.go:5:func TestAdd(t *testing.T) {")
	if strings.Contains(out, "data.bin") {
		t.Errorf("grep 应跳过二进制: %q", out)
	}

	out = r.Execute(context.Background(), "grep", `{"pattern":"func","include":"*_test.go"}`)
	mustContain(t, out, "util_test.go")
	if strings.Contains(out, "main.go:") {
		t.Errorf("include 过滤失败: %q", out)
	}

	out = r.Execute(context.Background(), "grep", `{"pattern":"[无效"}`)
	mustContain(t, out, "正则非法")

	out = r.Execute(context.Background(), "grep", `{"pattern":"指定不存在的内容xyz"}`)
	mustContain(t, out, "未找到匹配")
}

func TestBash(t *testing.T) {
	root := setupRoot(t)
	r := NewRegistry(root, 0)

	out := r.Execute(context.Background(), "run_bash", `{"command":"echo hi && ls"}`)
	mustContain(t, out, "hi")
	mustContain(t, out, "main.go")
	mustContain(t, out, "[成功")

	out = r.Execute(context.Background(), "run_bash", `{"command":"exit 3"}`)
	mustContain(t, out, "[退出码 3")

	out = r.Execute(context.Background(), "run_bash", `{"command":"sleep 2","timeout_seconds":1}`)
	mustContain(t, out, "超时")

	// 工作目录正确
	out = r.Execute(context.Background(), "run_bash", `{"command":"pwd"}`)
	if !strings.Contains(out, root) {
		t.Errorf("bash 应在 root 下执行, root=%s, out=%q", root, out)
	}

	// 输出截断
	out = r.Execute(context.Background(), "run_bash", `{"command":"head -c 100000 /dev/zero | tr '\\0' 'x'"}`)
	mustContain(t, out, "输出已截断")
}

func TestBashContextCancel(t *testing.T) {
	r := NewRegistry(setupRoot(t), 0)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	out := r.Execute(ctx, "run_bash", `{"command":"sleep 5"}`)
	if time.Since(start) > 2*time.Second {
		t.Errorf("取消未及时生效: %q", out)
	}
}

func TestUnknownTool(t *testing.T) {
	r := NewRegistry(setupRoot(t), 0)
	out := r.Execute(context.Background(), "nope", `{}`)
	mustContain(t, out, "未知工具")
}

func TestBadArgsJSON(t *testing.T) {
	r := NewRegistry(setupRoot(t), 0)
	out := r.Execute(context.Background(), "read_file", `{bad json`)
	mustContain(t, out, "参数 JSON 非法")
}

func TestTrimLine(t *testing.T) {
	long := strings.Repeat("x", 300)
	got := trimLine(long)
	if !strings.HasSuffix(got, "...") || len([]rune(got)) > 244 {
		t.Errorf("trimLine 结果异常: %d 字符", len([]rune(got)))
	}
	if trimLine("a\r") != "a" {
		t.Errorf("应去除 CR")
	}
}

func TestIsBinary(t *testing.T) {
	if !isBinary([]byte("abc\x00def")) {
		t.Error("含 NUL 应判定为二进制")
	}
	if isBinary([]byte("普通文本")) {
		t.Error("纯文本不应判定为二进制")
	}
}
