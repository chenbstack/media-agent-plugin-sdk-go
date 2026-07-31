package pluginsdk

import (
	"os"
	"path/filepath"
	"testing"
)

// 没声明 workspace.local 的插件拿到 nil。方法必须报错而不是 panic——插件里
// `inst.Workspace.WriteFile(...)` 这一行不该把整个宿主带崩。
func TestNilWorkspaceReportsMissingPermissionInsteadOfPanicking(t *testing.T) {
	var w *Workspace
	if w.Root() != "" {
		t.Errorf("nil 工作目录的 Root 应为空串")
	}
	if _, err := w.Path("cache"); err == nil {
		t.Error("nil 工作目录应报错")
	}
	if err := w.WriteFile("cache/x", []byte("x")); err == nil {
		t.Error("nil 工作目录写入应报错")
	}
	if NewWorkspace("  ") != nil {
		t.Error("空 root 应得到 nil，好让宿主直接赋值给 Instance.Workspace")
	}
}

func TestWorkspaceRejectsPathsThatLeaveTheRoot(t *testing.T) {
	w := NewWorkspace(t.TempDir())
	for _, rel := range []string{
		"../escape",
		"cache/../../escape",
		"/etc/passwd",
		"a/b/../../../c",
	} {
		if _, err := w.Path(rel); err == nil {
			t.Errorf("路径 %q 应当被拒绝", rel)
		}
	}
	// Clean 之后仍在根下面的相对路径要放行。
	if _, err := w.Path("cache/../models/x.gguf"); err != nil {
		t.Errorf("根内的路径不该被拒绝: %v", err)
	}
}

func TestWorkspaceCreatesParentDirectories(t *testing.T) {
	root := t.TempDir()
	w := NewWorkspace(root)

	if err := w.WriteFile("models/qwen/x.gguf", []byte("weights")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := w.ReadFile("models/qwen/x.gguf")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "weights" {
		t.Fatalf("内容 = %q", string(got))
	}
	if _, err := os.Stat(filepath.Join(root, "models", "qwen", "x.gguf")); err != nil {
		t.Fatalf("文件未落在工作目录里: %v", err)
	}
}

// 清空工作目录是插件的常规动作（上次跑剩的临时文件）。删掉根目录本身会让下一次
// 写入全部失败，直到宿主重启——所以 rel 为空时只清内容。
func TestWorkspaceRemoveAllKeepsTheRootDirectory(t *testing.T) {
	root := t.TempDir()
	w := NewWorkspace(root)
	if err := w.WriteFile("tmp/a.bin", []byte("a")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := w.RemoveAll(""); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("工作目录本身应当还在: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("工作目录未清空: %v", entries)
	}
}
